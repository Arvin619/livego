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
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"
	"net"
	"net/http"
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
	s.grpcServer.Serve(l)

	return nil
}

func (s *Server) Shutdown() {
	s.grpcServer.GracefulStop()
}

func HTTPStatusFromCode(code codes.Code) int {
	switch code {
	case codes.OK:
		return http.StatusOK
	case codes.Canceled:
		return 499
	case codes.Unknown:
		return http.StatusInternalServerError
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists:
		return http.StatusConflict
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.FailedPrecondition:
		// Note, this deliberately doesn't translate to the similarly named '412 Precondition Failed' HTTP response status.
		return http.StatusBadRequest
	case codes.Aborted:
		return http.StatusConflict
	case codes.OutOfRange:
		return http.StatusBadRequest
	case codes.Unimplemented:
		return http.StatusNotImplemented
	case codes.Internal:
		return http.StatusInternalServerError
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.DataLoss:
		return http.StatusInternalServerError
	default:
		grpclog.Infof("Unknown gRPC error code: %v", code)
		return http.StatusInternalServerError
	}
}
