package cli

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/fgmacedo/buchecha/internal/api"
	"github.com/fgmacedo/buchecha/internal/services"
)

// runListener bundles the run-wide HTTP surface: the api.Server that
// owns the single listener, the run-wide MCP boot mounted under /mcp/,
// the per-run session token, and the resolved listener address.
//
// One runListener is constructed per `bcc run`. The composition root
// invokes start to bind the listener, sets the MCP base URL on the
// boot once the bind succeeds, and calls stop on tear-down.
type runListener struct {
	boot         *mcpBoot
	apiServer    *api.Server
	sessionToken string
	addr         string

	stop func() error
}

// startRunListener binds a TCP listener at bind, mounts the MCP
// handler at /mcp/ on the api.Server, plumbs the session token onto
// the /api/v1/* and / subtrees, and propagates the bound address to
// the boot so executorMCPConfig hands agents a usable URL. webuiHandler
// is optional and may be nil; nil leaves the / subtree unmounted (chi
// returns the default 404).
//
// The returned listener owns its serve goroutine: stop cancels the
// listen context, waits for the goroutine to drain, and returns any
// terminal serve error.
//
// boot is the run-wide MCP plumbing the listener mounts at /mcp/. nil
// triggers an internal newMCPBoot(nil) fallback so tests that do not
// care about the boot's identity (most listener tests) stay terse;
// production callers build their own boot beforehand so the services
// aggregator can read its DAGHandler. svc may be nil; the API handlers
// short-circuit to ErrInternal "services not configured" until the
// composition root supplies a real aggregate.
func startRunListener(
	ctx context.Context,
	boot *mcpBoot,
	svc *services.Services,
	webuiHandler http.Handler,
	bind string,
) (*runListener, error) {
	if boot == nil {
		var err error
		boot, err = newMCPBoot(nil)
		if err != nil {
			return nil, err
		}
	}

	sessionToken := api.NewSessionToken()
	apiServer := api.New(svc).
		WithMounts(api.Mounts{
			MCP:     boot.server.Routes(),
			MCPAuth: api.MCPAuth(boot.token(), boot.server.ConnectionNames()),
			WebUI:   webuiHandler,
		}).
		WithAuth(sessionToken)

	listenCtx, cancelListen := context.WithCancel(ctx)
	addrCh := make(chan string, 1)
	bindErrCh := make(chan error, 1)

	var (
		serveErr  error
		serveOnce sync.Once
		serveDone = make(chan struct{})
	)
	go func() {
		defer close(serveDone)
		err := apiServer.ListenAndNotify(listenCtx, bind, func(addr string) {
			addrCh <- addr
			close(addrCh)
		})
		serveOnce.Do(func() { serveErr = err })
		// On a failed bind the ready callback never fires; surface the
		// error to startRunListener so it can return synchronously.
		select {
		case bindErrCh <- err:
		default:
		}
	}()

	select {
	case addr := <-addrCh:
		boot.setBaseURL(addr)
		stop := func() error {
			cancelListen()
			<-serveDone
			return serveErr
		}
		return &runListener{
			boot:         boot,
			apiServer:    apiServer,
			sessionToken: sessionToken,
			addr:         addr,
			stop:         stop,
		}, nil
	case err := <-bindErrCh:
		cancelListen()
		<-serveDone
		_ = boot.Close()
		if err == nil {
			err = fmt.Errorf("api: listener exited before binding")
		}
		return nil, err
	case <-ctx.Done():
		cancelListen()
		<-serveDone
		_ = boot.Close()
		return nil, ctx.Err()
	}
}

// Stop tears the listener down, draining the serve goroutine, then
// closes the boot. Safe to call multiple times; subsequent calls are
// no-ops.
func (l *runListener) Stop() error {
	if l == nil {
		return nil
	}
	var stopErr error
	if l.stop != nil {
		stopErr = l.stop()
		l.stop = nil
	}
	if cerr := l.boot.Close(); cerr != nil && stopErr == nil {
		stopErr = cerr
	}
	return stopErr
}
