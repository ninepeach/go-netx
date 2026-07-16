package mux

import (
	"context"
	"errors"
	"net"
	"sync"
)

type Server struct {
	Factory ServerFactory
	Handler StreamHandler
	OnError func(error)
}

func (s *Server) ServeConn(ctx context.Context, conn net.Conn) error {
	if s.Factory == nil || s.Handler == nil {
		return errors.New("mux: nil factory or handler")
	}
	session, err := s.Factory.AcceptSession(ctx, conn)
	if err != nil {
		return err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() {
		cancel()
		_ = session.Close()
		wg.Wait()
	}()
	for {
		stream, err := session.AcceptStream(streamCtx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer stream.Close()
			if err := s.Handler.ServeStream(streamCtx, stream); err != nil && s.OnError != nil {
				s.OnError(err)
			}
		}()
	}
}
