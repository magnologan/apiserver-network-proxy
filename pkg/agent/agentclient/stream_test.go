package agentclient

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"k8s.io/klog"
	"sigs.k8s.io/apiserver-network-proxy/proto/agent"
)

func TestReconnectExits(t *testing.T) {
	server := newTestServer("localhost:8899") // random addr
	server.Start()
	defer server.Stop()

	time.Sleep(time.Millisecond)

	client, err := NewRedialableAgentClient("localhost:8899", grpc.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}

	err = client.Send(&agent.Packet{
		Type: agent.PacketType_DIAL_REQ,
	})
	if err != nil {
		t.Error(err)
	}

	client1 := make(chan bool)
	go func() {
		_, err := client.Recv()
		if err != nil {
			if err2, ok := err.(*ReconnectError); ok {
				err2.Wait()
				client1 <- true
			}
		}
	}()

	client2 := make(chan bool)
	go func() {
		_, err := client.Recv()
		if err != nil {
			if err2, ok := err.(*ReconnectError); ok {
				err2.Wait()
				client2 <- true
			}
		}
	}()

	client.interrupt()

	var got1 bool
	var got2 bool
	select {
	case got1 = <-client1:
	case <-time.After(time.Second):
	}
	select {
	case got2 = <-client2:
	case <-time.After(time.Second):
	}

	if !got1 || !got2 {
		t.Errorf("expect both clients get unblocked; not they don't (%t %t)", got1, got2)
	}
}

type testServer struct {
	addr       string
	grpcServer *grpc.Server
}

func newTestServer(addr string) *testServer {
	return &testServer{addr: addr}
}

func (s *testServer) Connect(stream agent.AgentService_ConnectServer) error {
	stopCh := make(chan error)

	// Recv only
	go func() {
		for {
			_, err := stream.Recv()
			if err == io.EOF {
				close(stopCh)
				return
			}
			if err != nil {
				klog.Warningf(">>> Stream read from frontend error: %v", err)
				close(stopCh)
				return
			}
		}
	}()

	return <-stopCh
}

func (s *testServer) Start() error {
	s.grpcServer = grpc.NewServer()
	agent.RegisterAgentServiceServer(s.grpcServer, s)
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", s.addr, err)
	}
	go s.grpcServer.Serve(lis)
	return nil
}

func (s *testServer) Stop() {
	if s.grpcServer != nil {
		s.grpcServer.Stop()
	}
}

func (s *testServer) Addr() string {
	return s.addr
}
