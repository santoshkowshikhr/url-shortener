package main

import (
	"go-api-server/api"
	grpcserver "go-api-server/grpc"
	"go-api-server/model"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	model.InitDB("./db/shortener.db")
	model.InitRedis()
	listner, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen, error: %v", err)
	}

	grpcServer := grpc.NewServer()
	api.RegisterShortenerServiceServer(grpcServer, &grpcserver.Server{})

	reflection.Register(grpcServer)

	log.Println("gRPC server is running at: 50051")
	grpcServer.Serve(listner)
}
