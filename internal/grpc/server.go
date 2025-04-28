package grpc

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/artilugio0/proxy-vibes/internal/grpc/proto"
	"github.com/artilugio0/proxy-vibes/internal/httpbytes"
	"github.com/artilugio0/proxy-vibes/internal/proxy"
	"google.golang.org/grpc"
)

type requestsReadOnlyChannels struct {
	name string

	originalRequests chan<- *http.Request
	ok               <-chan bool
}

type responsesReadOnlyChannels struct {
	name string

	originalResponses chan<- *http.Response
	ok                <-chan bool
}

type requestsChannels struct {
	name string

	originalRequests chan<- *http.Request
	modifiedRequests <-chan *http.Request
}

type responsesChannels struct {
	name string

	originalResponses chan<- *http.Response
	modifiedResponses <-chan *http.Response
}

// Server implements the ProxyService interface defined in the proto file.
type Server struct {
	proto.UnimplementedProxyServiceServer

	addr string

	proxy       *proxy.Proxy
	configMutex sync.RWMutex
	config      *proxy.Config

	requestInClientsMutex sync.RWMutex
	requestInClients      map[string]*requestsReadOnlyChannels

	requestModClientsMutex sync.RWMutex
	requestModClients      map[string]*requestsChannels

	requestOutClientsMutex sync.RWMutex
	requestOutClients      map[string]*requestsReadOnlyChannels

	responseInClientsMutex sync.RWMutex
	responseInClients      map[string]*responsesReadOnlyChannels

	responseModClientsMutex sync.RWMutex
	responseModClients      map[string]*responsesChannels

	responseOutClientsMutex sync.RWMutex
	responseOutClients      map[string]*responsesReadOnlyChannels
}

func NewServer(addr string, p *proxy.Proxy, config *proxy.Config) *Server {
	server := &Server{
		addr:        addr,
		proxy:       p,
		config:      config,
		configMutex: sync.RWMutex{},

		requestInClients:      map[string]*requestsReadOnlyChannels{},
		requestInClientsMutex: sync.RWMutex{},

		requestModClients:      map[string]*requestsChannels{},
		requestModClientsMutex: sync.RWMutex{},

		requestOutClients:      map[string]*requestsReadOnlyChannels{},
		requestOutClientsMutex: sync.RWMutex{},

		responseInClients:      map[string]*responsesReadOnlyChannels{},
		responseInClientsMutex: sync.RWMutex{},

		responseModClients:      map[string]*responsesChannels{},
		responseModClientsMutex: sync.RWMutex{},

		responseOutClients:      map[string]*responsesReadOnlyChannels{},
		responseOutClientsMutex: sync.RWMutex{},
	}

	return server
}

func (s *Server) Run() {
	// Listen on a TCP port.
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	// Create a new gRPC Server.
	const maxMsgSize = 1024 * 1024 * 1024 // 10MB
	gs := grpc.NewServer(
		grpc.MaxRecvMsgSize(maxMsgSize),
		grpc.MaxSendMsgSize(maxMsgSize),
	)

	// Register the ProxyService implementation.
	proto.RegisterProxyServiceServer(gs, s)

	log.Printf("Starting gRPC Server on %s", s.addr)
	if err := gs.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

// RequestMod handles bidirectional streaming for HTTP request modification.
func (s *Server) RequestMod(stream proto.ProxyService_RequestModServer) error {
	clientMsg, err := stream.Recv()
	if err == io.EOF {
		log.Println("RequestMod stream closed by client")
		return nil
	}
	if err != nil {
		log.Printf("RequestMod error: %v", err)
		return err
	}

	registerMsg, ok := clientMsg.Msg.(*proto.RequestModClientMessage_Register)
	if !ok {
		log.Println("Request mod stream: client did not send register message")
		return nil
	}
	log.Printf("RequestMod Client connected: %s", registerMsg.Register.Name)

	originalRequests := make(chan *http.Request, 1000)
	modifiedRequests := make(chan *http.Request)

	clientName := registerMsg.Register.Name
	rChans := &requestsChannels{
		name:             clientName,
		originalRequests: originalRequests,
		modifiedRequests: modifiedRequests,
	}

	s.requestModClientsMutex.Lock()
	if _, exists := s.requestModClients[clientName]; exists {
		s.requestModClientsMutex.Unlock()
		return fmt.Errorf("client already registered")
	}
	s.requestModClients[clientName] = rChans
	s.requestModClientsMutex.Unlock()

	defer func() {
		close(modifiedRequests)
	}()

	for r := range originalRequests {
		if err := stream.Send(ToProtoRequest(httpbytes.CloneRequest(r))); err != nil {
			log.Printf("Failed to send HttpRequest: %v", err)
			return err
		}

		clientMsg, err := stream.Recv()
		if err == io.EOF {
			log.Println("RequestMod stream closed by client")
			return nil
		}
		if err != nil {
			log.Printf("RequestMod error: %v", err)
			return err
		}

		modRequestMsg, ok := clientMsg.Msg.(*proto.RequestModClientMessage_ModifiedRequest)
		if !ok {
			log.Println("Request mod stream: client did not send http request message")
			return nil
		}

		modReq, err := FromProtoRequest(modRequestMsg.ModifiedRequest)
		if err != nil {
			log.Printf("Request mod stream: client sent an invalid request: %v", err)
			return nil
		}
		modifiedRequests <- modReq
	}

	return nil
}

// RequestIn handles server to client streaming for HTTP request communication.
func (s *Server) RequestIn(register *proto.Register, stream proto.ProxyService_RequestInServer) error {
	log.Printf("RequestIn Client connected: %s", register.Name)
	originalRequests := make(chan *http.Request, 1000)
	ok := make(chan bool)

	clientName := register.Name
	rChans := &requestsReadOnlyChannels{
		name:             clientName,
		originalRequests: originalRequests,
		ok:               ok,
	}

	s.requestInClientsMutex.Lock()
	if _, exists := s.requestInClients[clientName]; exists {
		s.requestInClientsMutex.Unlock()
		return fmt.Errorf("client already registered")
	}
	s.requestInClients[clientName] = rChans
	s.requestInClientsMutex.Unlock()

	defer func() {
		close(ok)
	}()

	for r := range originalRequests {
		if err := stream.Send(ToProtoRequest(httpbytes.CloneRequest(r))); err != nil {
			log.Printf("Failed to send HttpRequest: %v", err)
			return err
		}
		ok <- true
	}

	return nil
}

// RequestOut handles server to client streaming for HTTP request communication.
func (s *Server) RequestOut(register *proto.Register, stream proto.ProxyService_RequestOutServer) error {
	log.Printf("RequestOut Client connected: %s", register.Name)
	originalRequests := make(chan *http.Request, 1000)
	ok := make(chan bool)

	clientName := register.Name
	rChans := &requestsReadOnlyChannels{
		name:             clientName,
		originalRequests: originalRequests,
		ok:               ok,
	}

	s.requestOutClientsMutex.Lock()
	if _, exists := s.requestOutClients[clientName]; exists {
		s.requestOutClientsMutex.Unlock()
		return fmt.Errorf("client already registered")
	}
	s.requestOutClients[clientName] = rChans
	s.requestOutClientsMutex.Unlock()

	defer func() {
		close(ok)
	}()

	for r := range originalRequests {
		if err := stream.Send(ToProtoRequest(httpbytes.CloneRequest(r))); err != nil {
			log.Printf("Failed to send HttpRequest: %v", err)
			return err
		}
		ok <- true
	}

	return nil
}

// ResponseMod handles bidirectional streaming for HTTP response modification.
func (s *Server) ResponseMod(stream proto.ProxyService_ResponseModServer) error {
	clientMsg, err := stream.Recv()
	if err == io.EOF {
		log.Println("ResponseMod stream closed by client")
		return nil
	}
	if err != nil {
		log.Printf("ResponseMod error: %v", err)
		return err
	}

	registerMsg, ok := clientMsg.Msg.(*proto.ResponseModClientMessage_Register)
	if !ok {
		log.Println("Response mod stream: client did not send register message")
		return nil
	}
	log.Printf("ResponseMod Client connected: %s", registerMsg.Register.Name)

	originalResponses := make(chan *http.Response, 1000)
	modifiedResponses := make(chan *http.Response)

	clientName := registerMsg.Register.Name
	rChans := &responsesChannels{
		name:              clientName,
		originalResponses: originalResponses,
		modifiedResponses: modifiedResponses,
	}

	s.responseModClientsMutex.Lock()
	if _, exists := s.responseModClients[clientName]; exists {
		s.responseModClientsMutex.Unlock()
		return fmt.Errorf("client already registered")
	}
	s.responseModClients[clientName] = rChans
	s.responseModClientsMutex.Unlock()

	defer func() {
		close(modifiedResponses)
	}()

	for r := range originalResponses {
		if err := stream.Send(ToProtoResponse(httpbytes.CloneResponse(r))); err != nil {
			log.Printf("Failed to send HttpResponse: %v", err)
			return err
		}

		clientMsg, err := stream.Recv()
		if err == io.EOF {
			log.Println("ResponseMod stream closed by client")
			return nil
		}
		if err != nil {
			log.Printf("ResponseMod error: %v", err)
			return err
		}

		modResponseMsg, ok := clientMsg.Msg.(*proto.ResponseModClientMessage_ModifiedResponse)
		if !ok {
			log.Println("Response mod stream: client did not send http response message")
			return nil
		}

		modReq, err := FromProtoResponse(modResponseMsg.ModifiedResponse, r.Request)
		if err != nil {
			log.Printf("Response mod stream: client sent an invalid response: %v", err)
			return nil
		}
		modifiedResponses <- modReq
	}

	return nil
}

// ResponseIn handles server to client streaming for HTTP response communication.
func (s *Server) ResponseIn(register *proto.Register, stream proto.ProxyService_ResponseInServer) error {
	log.Printf("ResponseIn Client connected: %s", register.Name)
	originalResponses := make(chan *http.Response, 1000)
	ok := make(chan bool)

	clientName := register.Name
	rChans := &responsesReadOnlyChannels{
		name:              clientName,
		originalResponses: originalResponses,
		ok:                ok,
	}

	s.responseInClientsMutex.Lock()
	if _, exists := s.responseInClients[clientName]; exists {
		s.responseInClientsMutex.Unlock()
		return fmt.Errorf("client already registered")
	}
	s.responseInClients[clientName] = rChans
	s.responseInClientsMutex.Unlock()

	defer func() {
		close(ok)
	}()

	for r := range originalResponses {
		if err := stream.Send(ToProtoResponse(httpbytes.CloneResponse(r))); err != nil {
			log.Printf("Failed to send HttpResponse: %v", err)
			return err
		}
		ok <- true
	}

	return nil
}

// ResponseOut handles server to client streaming for HTTP response communication.
func (s *Server) ResponseOut(register *proto.Register, stream proto.ProxyService_ResponseOutServer) error {
	log.Printf("ResponseOut Client connected: %s", register.Name)
	originalResponses := make(chan *http.Response, 1000)
	ok := make(chan bool)

	clientName := register.Name
	rChans := &responsesReadOnlyChannels{
		name:              clientName,
		originalResponses: originalResponses,
		ok:                ok,
	}

	s.responseOutClientsMutex.Lock()
	if _, exists := s.responseOutClients[clientName]; exists {
		s.responseOutClientsMutex.Unlock()
		return fmt.Errorf("client already registered")
	}
	s.responseOutClients[clientName] = rChans
	s.responseOutClientsMutex.Unlock()

	defer func() {
		close(ok)
	}()

	for r := range originalResponses {
		if err := stream.Send(ToProtoResponse(httpbytes.CloneResponse(r))); err != nil {
			log.Printf("Failed to send HttpResponse: %v", err)
			return err
		}
		ok <- true
	}

	return nil
}

func (s *Server) RequestInHook(r *http.Request) error {
	var clients []*requestsReadOnlyChannels
	s.requestInClientsMutex.RLock()
	for _, rc := range s.requestInClients {
		clients = append(clients, rc)
	}
	// TODO: order by priority
	s.requestInClientsMutex.RUnlock()

	for _, client := range clients {
		go func(client *requestsReadOnlyChannels) {
		SELECT:
			select {
			case client.originalRequests <- r:
				break SELECT
			default:
				log.Printf("Queue full, client '%s' removed", client.name)
				s.requestInClientsMutex.Lock()
				delete(s.requestInClients, client.name)
				s.requestInClientsMutex.Unlock()

				asyncCloseChannel(client.originalRequests)
			}

			if !<-client.ok {
				log.Printf("Empty response, client '%s' removed", client.name)
				s.requestInClientsMutex.Lock()
				delete(s.requestInClients, client.name)
				s.requestInClientsMutex.Unlock()

				asyncCloseChannel(client.originalRequests)
			}
		}(client)
	}

	return nil
}

func (s *Server) RequestOutHook(r *http.Request) error {
	var clients []*requestsReadOnlyChannels
	s.requestOutClientsMutex.RLock()
	for _, rc := range s.requestOutClients {
		clients = append(clients, rc)
	}
	// TODO: order by priority
	s.requestOutClientsMutex.RUnlock()

	for _, client := range clients {
		go func(client *requestsReadOnlyChannels) {
		SELECT:
			select {
			case client.originalRequests <- r:
				break SELECT
			default:
				log.Printf("Queue full, client '%s' removed", client.name)
				s.requestOutClientsMutex.Lock()
				delete(s.requestOutClients, client.name)
				s.requestOutClientsMutex.Unlock()

				asyncCloseChannel(client.originalRequests)
			}

			if !<-client.ok {
				log.Printf("Empty response, client '%s' removed", client.name)
				s.requestOutClientsMutex.Lock()
				delete(s.requestOutClients, client.name)
				s.requestOutClientsMutex.Unlock()

				asyncCloseChannel(client.originalRequests)
			}
		}(client)
	}

	return nil
}

func (s *Server) RequestModHook(r *http.Request) (*http.Request, error) {
	var clients []*requestsChannels
	s.requestModClientsMutex.RLock()
	for _, rc := range s.requestModClients {
		clients = append(clients, rc)
	}
	// TODO: order by priority
	s.requestModClientsMutex.RUnlock()

FOR:
	for _, client := range clients {
	SELECT:
		select {
		case client.originalRequests <- r:
			break SELECT
		default:
			log.Printf("Queue full, client '%s' removed", client.name)
			s.requestModClientsMutex.Lock()
			delete(s.requestModClients, client.name)
			s.requestModClientsMutex.Unlock()
			continue FOR

			asyncCloseChannel(client.originalRequests)
		}

		r := <-client.modifiedRequests
		if r == nil {
			log.Printf("Empty response, client '%s' removed", client.name)
			s.requestModClientsMutex.Lock()
			delete(s.requestModClients, client.name)
			s.requestModClientsMutex.Unlock()

			asyncCloseChannel(client.originalRequests)
		}
	}

	return r, nil
}

func (s *Server) ResponseInHook(r *http.Response) error {
	var clients []*responsesReadOnlyChannels
	s.responseInClientsMutex.RLock()
	for _, rc := range s.responseInClients {
		clients = append(clients, rc)
	}
	// TODO: order by priority
	s.responseInClientsMutex.RUnlock()

	for _, client := range clients {
		go func(client *responsesReadOnlyChannels) {
		SELECT:
			select {
			case client.originalResponses <- r:
				break SELECT
			default:
				log.Printf("Queue full, client '%s' removed", client.name)
				s.responseInClientsMutex.Lock()
				delete(s.responseInClients, client.name)
				s.responseInClientsMutex.Unlock()

				asyncCloseChannel(client.originalResponses)
			}

			if !<-client.ok {
				log.Printf("Empty response, client '%s' removed", client.name)
				s.responseInClientsMutex.Lock()
				delete(s.responseInClients, client.name)
				s.responseInClientsMutex.Unlock()

				asyncCloseChannel(client.originalResponses)
			}
		}(client)
	}

	return nil
}

func (s *Server) ResponseOutHook(r *http.Response) error {
	var clients []*responsesReadOnlyChannels
	s.responseOutClientsMutex.RLock()
	for _, rc := range s.responseOutClients {
		clients = append(clients, rc)
	}
	// TODO: order by priority
	s.responseOutClientsMutex.RUnlock()

	for _, client := range clients {
		go func(client *responsesReadOnlyChannels) {
		SELECT:
			select {
			case client.originalResponses <- r:
				break SELECT
			default:
				log.Printf("Queue full, client '%s' removed", client.name)
				s.responseOutClientsMutex.Lock()
				delete(s.responseOutClients, client.name)
				s.responseOutClientsMutex.Unlock()

				asyncCloseChannel(client.originalResponses)
			}

			if !<-client.ok {
				log.Printf("Empty response, client '%s' removed", client.name)
				s.responseOutClientsMutex.Lock()
				delete(s.responseOutClients, client.name)
				s.responseOutClientsMutex.Unlock()

				asyncCloseChannel(client.originalResponses)
			}
		}(client)
	}

	return nil
}

func (s *Server) ResponseModHook(r *http.Response) (*http.Response, error) {
	var clients []*responsesChannels
	s.responseModClientsMutex.RLock()
	for _, rc := range s.responseModClients {
		clients = append(clients, rc)
	}
	// TODO: order by priority
	s.responseModClientsMutex.RUnlock()

FOR:
	for _, client := range clients {
	SELECT:
		select {
		case client.originalResponses <- r:
			break SELECT
		default:
			log.Printf("Queue full, client '%s' removed", client.name)
			s.responseModClientsMutex.Lock()
			delete(s.responseModClients, client.name)
			s.responseModClientsMutex.Unlock()

			asyncCloseChannel(client.originalResponses)
			continue FOR
		}

		r := <-client.modifiedResponses
		if r == nil {
			log.Printf("Empty response, client '%s' removed", client.name)
			s.responseModClientsMutex.Lock()
			delete(s.responseModClients, client.name)
			s.responseModClientsMutex.Unlock()

			asyncCloseChannel(client.originalResponses)
		}
	}

	return r, nil
}

// GetConfig returns the current proxy config
func (s *Server) GetConfig(ctx context.Context, _ *proto.Null) (*proto.Config, error) {
	s.configMutex.RLock()
	defer s.configMutex.RUnlock()

	config := &proto.Config{
		DbFile:                  s.config.DBFile,
		PrintLogs:               s.config.PrintLogs,
		SaveDir:                 s.config.SaveDir,
		ScopeDomainRe:           s.config.DomainRe,
		ScopeExcludedExtensions: s.config.ExcludedExtensions,
	}

	return config, nil
}

// SetConfig returns the current proxy config
func (s *Server) SetConfig(ctx context.Context, config *proto.Config) (*proto.Null, error) {
	s.configMutex.Lock()
	newConfig := *s.config
	defer s.configMutex.Unlock()

	newConfig.DBFile = config.DbFile
	newConfig.PrintLogs = config.PrintLogs
	newConfig.SaveDir = config.SaveDir
	newConfig.DomainRe = config.ScopeDomainRe
	newConfig.ExcludedExtensions = config.ScopeExcludedExtensions

	newConfig.Apply(s.proxy)
	s.config = &newConfig

	return &proto.Null{}, nil
}

func asyncCloseChannel[I any](c chan<- I) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("double close on channel", r)
			}
		}()
		time.Sleep(60 * time.Second)
		log.Printf("channel closed")
		close(c)
	}()
}

var closedChannels *sync.Map = &sync.Map{}
