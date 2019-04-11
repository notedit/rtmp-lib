package main

import (
	"io"
	"net/http"
	"sync"

	"github.com/notedit/rtmp-lib/av"
	"github.com/notedit/rtmp-lib/flv"

	rtmp "github.com/notedit/rtmp-lib"
	"github.com/notedit/rtmp-lib/pubsub"
)

type Channel struct {
	que *pubsub.Queue
}

var channels = map[string]*Channel{}

type writeFlusher struct {
	httpflusher http.Flusher
	io.Writer
}

func (self writeFlusher) Flush() error {
	self.httpflusher.Flush()
	return nil
}

func main() {

	l := &sync.RWMutex{}

	server := rtmp.NewServer(1024)

	server.HandlePlay = func(conn *rtmp.Conn) {

		l.RLock()
		ch := channels[conn.URL.Path]
		l.RUnlock()

		if ch != nil {

			cursor := ch.que.Latest()

			streams, err := cursor.Streams()

			if err != nil {
				panic(err)
			}

			conn.WriteHeader(streams)

			for {
				packet, err := cursor.ReadPacket()
				if err != nil {
					break
				}
				conn.WritePacket(packet)
			}
		}
	}

	server.HandlePublish = func(conn *rtmp.Conn) {

		l.Lock()
		ch := channels[conn.URL.Path]

		if ch == nil {
			ch = &Channel{}
			ch.que = pubsub.NewQueue()
			ch.que.SetMaxGopCount(1)
			channels[conn.URL.Path] = ch
		}
		l.Unlock()

		var streams []av.CodecData
		var err error

		if streams, err = conn.Streams(); err != nil {
			panic(err)
		}

		ch.que.WriteHeader(streams)

		for {
			var pkt av.Packet
			if pkt, err = conn.ReadPacket(); err != nil {
				break
			}

			ch.que.WritePacket(pkt)
		}

		l.Lock()
		delete(channels, conn.URL.Path)
		l.Unlock()

		ch.que.Close()
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {

		l.RLock()
		ch := channels[r.URL.Path]
		l.RUnlock()

		if ch != nil {
			w.Header().Set("Content-Type", "video/x-flv")
			w.Header().Set("Transfer-Encoding", "chunked")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.WriteHeader(200)
			flusher := w.(http.Flusher)
			flusher.Flush()

			muxer := flv.NewMuxerWriteFlusher(writeFlusher{httpflusher: flusher, Writer: w})
			cursor := ch.que.Latest()

			streams, err := cursor.Streams()

			if err != nil {
				panic(err)
			}

			muxer.WriteHeader(streams)

			for {
				packet, err := cursor.ReadPacket()
				if err != nil {
					break
				}
				muxer.WritePacket(packet)
			}
		} else {
			http.NotFound(w, r)
		}
	})

	go http.ListenAndServe(":8088", nil)

	server.ListenAndServe()

}
