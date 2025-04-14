package main

import (
	"context"
	"fmt"
	"io"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/artilugio0/proxy-vibes/internal/grpc/proto"
)

func main() {
	clientName := "test-client"

	// Connect to gRPC server
	const maxMsgSize = 1024 * 1024 * 1024 // 10MB
	conn, err := grpc.Dial(
		"localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxMsgSize),
			grpc.MaxCallSendMsgSize(maxMsgSize),
		),
	)

	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewProxyServiceClient(conn)

	// Channel to handle graceful shutdown
	done := make(chan struct{})

	// Start bidirectional streaming
	go requestInClient(client, clientName, done)
	go requestModClient(client, clientName, done)
	go requestOutClient(client, clientName, done)
	go responseInClient(client, clientName, done)
	go responseModClient(client, clientName, done)
	go responseOutClient(client, clientName, done)

	// Keep the client running until interrupted
	select {
	case <-done:
		log.Println("Client shutting down")
	}
}

func requestInClient(client pb.ProxyServiceClient, clientName string, done chan<- struct{}) {
	stream, err := client.RequestIn(context.TODO(), &pb.Register{Name: clientName})
	if err != nil {
		log.Fatalf("Failed to start stream: %v", err)
	}
	fmt.Printf("Registered client: %s", clientName)

	// Goroutine to receive server messages
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			log.Println("Server closed stream")
			close(done)
			return
		}
		if err != nil {
			log.Printf("Stream error: %v", err)
			close(done)
			return
		}

		fmt.Printf("Received Request:\n  RequestID: %s\n  Method: %s\n  URL: %s\n  Headers: %v\n",
			req.Id, req.Method, req.Url, req.Headers)
	}
}

func requestModClient(client pb.ProxyServiceClient, clientName string, done chan<- struct{}) {
	stream, err := client.RequestMod(context.TODO())
	if err != nil {
		log.Fatalf("Failed to start stream: %v", err)
	}

	// Send client registration info
	if err := stream.Send(&pb.RequestModClientMessage{
		Msg: &pb.RequestModClientMessage_Register{
			Register: &pb.Register{
				Name: clientName,
			},
		},
	}); err != nil {
		log.Fatalf("Failed to send registration message: %v", err)
	}
	fmt.Printf("Registered client: %s", clientName)

	// Goroutine to receive server messages
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			log.Println("Server closed stream")
			close(done)
			return
		}
		if err != nil {
			log.Printf("Stream error: %v", err)
			close(done)
			return
		}

		fmt.Printf("Received Request:\n  RequestID: %s\n  Method: %s\n  URL: %s\n  Headers: %v\n",
			req.Id, req.Method, req.Url, req.Headers)

		// Echo back the same request as ModifiedRequest
		modifiedReq := req
		if err := stream.Send(&pb.RequestModClientMessage{
			Msg: &pb.RequestModClientMessage_ModifiedRequest{ModifiedRequest: modifiedReq},
		}); err != nil {
			log.Printf("Failed to send modified request: %v", err)
		} else {
			fmt.Printf("Sent ModifiedRequest for RequestID: %s\n", req.Id)
		}

	}
}

func requestOutClient(client pb.ProxyServiceClient, clientName string, done chan<- struct{}) {
	stream, err := client.RequestOut(context.TODO(), &pb.Register{Name: clientName})
	if err != nil {
		log.Fatalf("Failed to start stream: %v", err)
	}
	fmt.Printf("Registered client: %s", clientName)

	// Goroutine to receive server messages
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			log.Println("Server closed stream")
			close(done)
			return
		}
		if err != nil {
			log.Printf("Stream error: %v", err)
			close(done)
			return
		}

		fmt.Printf("Received Request:\n  RequestID: %s\n  Method: %s\n  URL: %s\n  Headers: %v\n",
			req.Id, req.Method, req.Url, req.Headers)
	}
}

func responseModClient(client pb.ProxyServiceClient, clientName string, done chan<- struct{}) {
	stream, err := client.ResponseMod(context.TODO())
	if err != nil {
		log.Fatalf("Failed to start stream: %v", err)
	}

	// Send client registration info
	if err := stream.Send(&pb.ResponseModClientMessage{
		Msg: &pb.ResponseModClientMessage_Register{
			Register: &pb.Register{
				Name: clientName,
			},
		},
	}); err != nil {
		log.Fatalf("Failed to send registration message: %v", err)
	}
	fmt.Printf("Registered client: %s", clientName)

	// Goroutine to receive server messages
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			log.Println("Server closed stream")
			close(done)
			return
		}
		if err != nil {
			log.Printf("Stream error: %v", err)
			close(done)
			return
		}

		fmt.Printf("Received Response:\n  ResponseID: %s\n  Status: %d\n  Headers: %v\n",
			resp.Id, resp.StatusCode, resp.Headers)

		// Echo back the same response as ModifiedResponse
		modifiedReq := resp
		if err := stream.Send(&pb.ResponseModClientMessage{
			Msg: &pb.ResponseModClientMessage_ModifiedResponse{ModifiedResponse: modifiedReq},
		}); err != nil {
			log.Printf("Failed to send modified response: %v", err)
		} else {
			fmt.Printf("Sent ModifiedResponse for ResponseID: %s\n", resp.Id)
		}

	}
}

func responseInClient(client pb.ProxyServiceClient, clientName string, done chan<- struct{}) {
	stream, err := client.ResponseIn(context.TODO(), &pb.Register{Name: clientName})
	if err != nil {
		log.Fatalf("Failed to start stream: %v", err)
	}
	fmt.Printf("Registered client: %s", clientName)

	// Goroutine to receive server messages
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			log.Println("Server closed stream")
			close(done)
			return
		}
		if err != nil {
			log.Printf("Stream error: %v", err)
			close(done)
			return
		}

		fmt.Printf("Received Response:\n  ResponseID: %s\n  Status: %d\n  Headers: %v\n",
			req.Id, req.StatusCode, req.Headers)
	}
}

func responseOutClient(client pb.ProxyServiceClient, clientName string, done chan<- struct{}) {
	stream, err := client.ResponseOut(context.TODO(), &pb.Register{Name: clientName})
	if err != nil {
		log.Fatalf("Failed to start stream: %v", err)
	}
	fmt.Printf("Registered client: %s", clientName)

	// Goroutine to receive server messages
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			log.Println("Server closed stream")
			close(done)
			return
		}
		if err != nil {
			log.Printf("Stream error: %v", err)
			close(done)
			return
		}

		fmt.Printf("Received Response:\n  ResponseID: %s\n  Status: %d\n  Headers: %v\n",
			req.Id, req.StatusCode, req.Headers)
	}
}
