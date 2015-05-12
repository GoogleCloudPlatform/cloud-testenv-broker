// Package main implements the fake gateway.
package main

import (
	"flag"
	"fmt"
	"google.golang.org/grpc"
	"log"
	"net"
	"regexp"

	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
	g "google/fakes"
	google_protobuf "google/protobuf"
)

var (
	tls      = flag.Bool("tls", false, "Connection uses TLS if true, else plain TCP")
	certFile = flag.String("cert_file", "server1.pem", "The TLS cert file")
	keyFile  = flag.String("key_file", "server1.key", "The TLS key file")
	port     = flag.Int("port", 10000, "The server port")
	EMPTY    = &google_protobuf.Empty{}
)

type server struct{}

type cmdSpec struct {
	regexp string
	path   string
	args   []string
}

type matcher struct {
	regexp string
	target string
}

// This maps the url patterns to targets urls.
// this is a list as the evaluation order matters.
var activeFakes []matcher

// This maps the url patterns to cmd to start to have the fake
var ondemandFakes []cmdSpec

func init() {
	activeFakes = make([]matcher, 0, 10)
	ondemandFakes = make([]cmdSpec, 0, 10)
}

// we implement the gateway here
func (s *server) Register(ctx context.Context, req *g.RegisterRequest) (*g.RegisterResponse, error) {
	log.Printf("Register req %q", req)
	if req.Registration.ResolvedTarget != "" {
		activeFakes = append(activeFakes, matcher{
			regexp: req.Registration.TargetPattern,
			target: req.Registration.ResolvedTarget,
		})
	} else {
		log.Printf("TODO: implement")
	}
	return &g.RegisterResponse{}, nil
}

func (s *server) Resolve(ctx context.Context, req *g.ResolveRequest) (*g.ResolveResponse, error) {
	log.Printf("Resolve %q", req)
	target := []byte(req.Target)
	for _, matcher := range activeFakes {
		matched, err := regexp.Match(matcher.regexp, target)
		if err != nil {
			return nil, err
		}
		if matched {
			res := &g.ResolveResponse{
				ResolvedTarget: matcher.target,
			}
			return res, nil
		}
	}
	return nil, fmt.Errorf("%s not found", req.Target)
}

func (s *server) Ping(ctx context.Context, e *google_protobuf.Empty) (*google_protobuf.Empty, error) {
	log.Println("Ping")
	return EMPTY, nil
}

func main() {
	log.Printf("Fakes Gateway starting up...")
	flag.Parse()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v.", err)
	}
	grpcServer := grpc.NewServer()
	server := server{}
	g.RegisterGatewayServer(grpcServer, &server)
	if *tls {
		creds, err := credentials.NewServerTLSFromFile(*certFile, *keyFile)
		if err != nil {
			log.Fatalf("Failed to generate credentials %v.", err)
		}
		log.Printf("Gateway listening with TLS on :%d.", *port)
		grpcServer.Serve(creds.NewListener(lis))
	} else {
		log.Printf("Gateway listening on :%d.", *port)
		grpcServer.Serve(lis)
	}
}
