package hls

import (
	"fmt"
	pb "github.com/Arvin619/livego/protocol/grpc/proto"
	googlegprc "google.golang.org/grpc"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Arvin619/livego/configure"

	"github.com/Arvin619/livego/av"
	"github.com/Arvin619/livego/protocol/grpc"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	grpcStatus "google.golang.org/grpc/status"
)

const (
	duration = 3000
)

const (
	prePath = "/source"
)

var (
	ErrNoPublisher         = fmt.Errorf("no publisher")
	ErrInvalidReq          = fmt.Errorf("invalid req url path")
	ErrNoSupportVideoCodec = fmt.Errorf("no support video codec")
	ErrNoSupportAudioCodec = fmt.Errorf("no support audio codec")
)

var crossdomainxml = []byte(`<?xml version="1.0" ?>
<cross-domain-policy>
	<allow-access-from domain="*" />
	<allow-http-request-headers-from domain="*" headers="*"/>
</cross-domain-policy>`)

type Server struct {
	listener net.Listener
	conns    *sync.Map

	backstageManagerClient pb.BackstageManagerClient
}

func NewServer(conn *googlegprc.ClientConn) *Server {
	ret := &Server{
		conns:                  &sync.Map{},
		backstageManagerClient: pb.NewBackstageManagerClient(conn),
	}
	go ret.checkStop()
	return ret
}

func (server *Server) Serve(listener net.Listener) error {
	// mux := http.NewServeMux()
	// mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
	// 	server.handle(w, r)
	// })

	mux := server.setRoute()

	server.listener = listener

	if configure.Config.GetBool("use_hls_https") {
		http.ServeTLS(listener, mux, "server.crt", "server.key")
	} else {
		http.Serve(listener, mux)
	}

	return nil
}

func (server *Server) GetWriter(info av.Info) av.WriteCloser {
	var s *Source
	v, ok := server.conns.Load(info.Key)
	if !ok {
		log.Debug("new hls source")
		s = NewSource(info)
		server.conns.Store(info.Key, s)
	} else {
		s = v.(*Source)
	}
	return s
}

func (server *Server) getConn(key string) *Source {
	v, ok := server.conns.Load(key)
	if !ok {
		return nil
	}
	return v.(*Source)
}

func (server *Server) checkStop() {
	for {
		<-time.After(5 * time.Second)

		server.conns.Range(func(key, val interface{}) bool {
			v := val.(*Source)
			if !v.Alive() && !configure.Config.GetBool("hls_keep_after_end") {
				log.Debug("check stop and remove: ", v.Info())
				server.conns.Delete(key)
			}
			return true
		})
	}
}

func (server *Server) handle(w http.ResponseWriter, r *http.Request) {
	if path.Base(r.URL.Path) == "crossdomain.xml" {
		w.Header().Set("Content-Type", "application/xml")
		w.Write(crossdomainxml)
		return
	}
	switch path.Ext(r.URL.Path) {
	case ".m3u8":
		key, _ := server.parseM3u8(r.URL.Path)
		conn := server.getConn(key)
		if conn == nil {
			http.Error(w, ErrNoPublisher.Error(), http.StatusForbidden)
			return
		}
		tsCache := conn.GetCacheInc()
		if tsCache == nil {
			http.Error(w, ErrNoPublisher.Error(), http.StatusForbidden)
			return
		}
		body, err := tsCache.GenM3U8PlayList()
		if err != nil {
			log.Debug("GenM3U8PlayList error: ", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "application/x-mpegURL")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	case ".ts":
		key, _ := server.parseTs(r.URL.Path)
		conn := server.getConn(key)
		if conn == nil {
			http.Error(w, ErrNoPublisher.Error(), http.StatusForbidden)
			return
		}
		tsCache := conn.GetCacheInc()
		item, err := tsCache.GetItem(r.URL.Path)
		if err != nil {
			log.Debug("GetItem error: ", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "video/mp2ts")
		w.Header().Set("Content-Length", strconv.Itoa(len(item.Data)))
		w.Write(item.Data)
	}
}

func (server *Server) parseM3u8(pathstr string) (key string, err error) {
	pathstr = strings.TrimLeft(pathstr, "/")
	key = strings.Split(pathstr, path.Ext(pathstr))[0]
	return
}

func (server *Server) parseTs(pathstr string) (key string, err error) {
	pathstr = strings.TrimLeft(pathstr, "/")
	paths := strings.SplitN(pathstr, "/", 3)
	if len(paths) != 3 {
		err = fmt.Errorf("invalid path=%s", pathstr)
		return
	}
	key = paths[0] + "/" + paths[1]

	return
}

func (server *Server) setRoute() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(server.crossdomain())
	r.GET(prePath+"/*filepath", server.getSource())

	apiV1 := r.Group("/api/v1")
	{
		apiV1.GET("/get", func(ctx *gin.Context) {
			appName := ctx.GetString("appName")
			if appName == "" {
				appName = "live"
			}

			room := ctx.Query("room")
			res, err := server.backstageManagerClient.GetRoomKey(ctx, &pb.GetRoomKeyRequest{
				AppName:     appName,
				RoomChannel: room,
			})
			if err != nil {
				statusErr, ok := grpcStatus.FromError(err)
				if !ok {
					ctx.JSON(http.StatusInternalServerError, gin.H{
						"data":  nil,
						"error": "Internal Server Error",
					})
					return
				} else if grpc.HTTPStatusFromCode(statusErr.Code()) == http.StatusBadRequest {
					ctx.JSON(http.StatusBadRequest, gin.H{
						"data":  nil,
						"error": "url: /api/v1/get?room=<ROOM_NAME>",
					})
					return
				} else {
					ctx.JSON(grpc.HTTPStatusFromCode(statusErr.Code()), gin.H{
						"data":  nil,
						"error": statusErr.Message(),
					})
					return
				}
			}
			ctx.JSON(http.StatusOK, gin.H{
				"data":  res.RoomKey,
				"error": nil,
			})
		})
	}
	return r
}

func (server *Server) crossdomain() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if path.Base(ctx.Request.URL.Path) == "crossdomain.xml" {
			// w.Header().Set("Content-Type", "application/xml")
			// w.Write(crossdomainxml)
			ctx.Header("Content-Type", "application/xml")
			ctx.Writer.Write(crossdomainxml)
			ctx.Abort()
			return
		}
		ctx.Next()
	}
}

func (server *Server) getSource() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		filePath := ctx.Param("filepath")
		switch path.Ext(filePath) {
		case ".m3u8":
			key, _ := server.parseM3u8(filePath)
			conn := server.getConn(key)
			if conn == nil {
				// http.Error(w, ErrNoPublisher.Error(), http.StatusForbidden)
				ctx.String(http.StatusForbidden, ErrNoPublisher.Error())
				return
			}
			tsCache := conn.GetCacheInc()
			if tsCache == nil {
				// http.Error(w, ErrNoPublisher.Error(), http.StatusForbidden)
				ctx.String(http.StatusForbidden, ErrNoPublisher.Error())
				return
			}
			body, err := tsCache.GenM3U8PlayListWithPrePath(prePath)
			if err != nil {
				log.Debug("GenM3U8PlayList error: ", err)
				// http.Error(w, err.Error(), http.StatusBadRequest)
				ctx.String(http.StatusBadRequest, err.Error())
				return
			}

			// w.Header().Set("Access-Control-Allow-Origin", "*")
			// w.Header().Set("Cache-Control", "no-cache")
			// w.Header().Set("Content-Type", "application/x-mpegURL")
			// w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			ctx.Header("Access-Control-Allow-Origin", "*")
			ctx.Header("Cache-Control", "no-cache")
			ctx.Header("Content-Type", "application/x-mpegURL")
			ctx.Header("Content-Length", strconv.Itoa(len(body)))
			ctx.Writer.Write(body)
		case ".ts":
			key, _ := server.parseTs(filePath)
			conn := server.getConn(key)
			if conn == nil {
				// http.Error(w, ErrNoPublisher.Error(), http.StatusForbidden)
				ctx.String(http.StatusForbidden, ErrNoPublisher.Error())
				return
			}
			tsCache := conn.GetCacheInc()
			if tsCache == nil {
				// http.Error(w, ErrNoPublisher.Error(), http.StatusForbidden)
				ctx.String(http.StatusForbidden, ErrNoPublisher.Error())
				return
			}
			item, err := tsCache.GetItem(filePath)
			if err != nil {
				log.Debug("GetItem error: ", err)
				// http.Error(w, err.Error(), http.StatusBadRequest)
				ctx.String(http.StatusBadRequest, err.Error())
				return
			}
			// w.Header().Set("Access-Control-Allow-Origin", "*")
			// w.Header().Set("Content-Type", "video/mp2ts")
			// w.Header().Set("Content-Length", strconv.Itoa(len(item.Data)))
			ctx.Header("Access-Control-Allow-Origin", "*")
			ctx.Header("Content-Type", "video/mp2ts")
			ctx.Header("Content-Length", strconv.Itoa(len(item.Data)))
			ctx.Writer.Write(item.Data)
		default:
			ctx.String(http.StatusNotFound, "file not found")
		}
	}
}
