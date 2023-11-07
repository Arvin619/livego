package grpc_test

import (
	"context"
	"github.com/Arvin619/livego/protocol/grpc"
	pb "github.com/Arvin619/livego/protocol/grpc/proto"
	"github.com/stretchr/testify/assert"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"net"
	"os"
	"testing"
)

var server *grpc.Server
var client pb.BackstageManagerClient
var conn *googlegrpc.ClientConn

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	teardown()
	os.Exit(code)
}

func setup() {
	server = grpc.NewServer(nil, "")
	l, err := net.Listen("tcp", ":8080")
	if err != nil {
		os.Exit(12)
	}
	go func() {
		server.Serve(l)
	}()

	conn, err = googlegrpc.Dial(":8080", googlegrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		os.Exit(13)
	}
	client = pb.NewBackstageManagerClient(conn)
}

func teardown() {
	conn.Close()
	server.Shutdown()
}

func TestGetRoomKeyError(t *testing.T) {
	_, err := client.GetRoomKey(context.Background(), &pb.GetRoomKeyRequest{AppName: "", RoomChannel: ""})
	assert.NotNil(t, err)
	statusErr, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, statusErr.Code(), codes.InvalidArgument)
	assert.Equal(t, statusErr.Message(), "AppName and RoomChannel must be not empty")
}
