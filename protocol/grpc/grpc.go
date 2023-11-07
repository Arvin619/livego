package grpc

import (
	"context"
	"errors"
	"github.com/Arvin619/livego/av"
	"github.com/Arvin619/livego/configure"
	pb "github.com/Arvin619/livego/protocol/grpc/proto"
	"github.com/Arvin619/livego/protocol/rtmp/rtmprelay"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net"
)

var _ pb.BackstageManagerServer = (*Server)(nil)

var (
	ErrBadRequest = errors.New("bad request")
)

type Server struct {
	handler  av.Handler
	session  map[string]*rtmprelay.RtmpRelay
	rtmpAddr string

	grpcServer *grpc.Server

	pb.UnimplementedBackstageManagerServer
}

func NewServer(h av.Handler, rtmpAddr string) *Server {
	s := &Server{
		handler:    h,
		session:    make(map[string]*rtmprelay.RtmpRelay),
		rtmpAddr:   rtmpAddr,
		grpcServer: grpc.NewServer(),
	}

	pb.RegisterBackstageManagerServer(s.grpcServer, s)

	return s
}

func (s *Server) GetRoomKey(ctx context.Context, request *pb.GetRoomKeyRequest) (*pb.GetRoomKeyResponse, error) {
	if request.AppName == "" || request.RoomChannel == "" {
		return nil, status.Error(codes.InvalidArgument, "AppName and RoomChannel must be not empty")
	}

	if !configure.CheckAppName(request.AppName) {
		return nil, status.Errorf(codes.NotFound, "application name=%s is not configured", request.AppName)
	}

	key, err := configure.RoomKeys.SetKey(request.RoomChannel)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &pb.GetRoomKeyResponse{RoomKey: key}, nil
}

func (s *Server) Serve(l net.Listener) error {
	defer func() {
		l.Close()
	}()
	s.grpcServer.Serve(l)

	return nil
}

func (s *Server) Shutdown() {
	s.grpcServer.GracefulStop()
}
