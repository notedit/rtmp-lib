package main

import (
	"fmt"
	"github.com/notedit/rtmp-lib/av"
	"time"

	rtmp "github.com/notedit/rtmp-lib"
)


var pubstream *rtmp.Conn
var playstream *rtmp.Conn

var start bool

func main() {

	config := &rtmp.Config{
		ChunkSize:  128,
		BufferSize: 0,
	}
	server := rtmp.NewServer(config)

	//rtmp.Debug = true

	server.HandlePlay = func(conn *rtmp.Conn) {

		playstream = conn

		streams,_ := pubstream.Streams()
		playstream.WriteHeader(streams)


		for {
			time.Sleep(time.Second)
		}
	}

	server.HandlePublish = func(conn *rtmp.Conn) {

		pubstream = conn

		_, err := conn.Streams()
		if err != nil {
			panic(err)
		}

		for {
			var pkt av.Packet
			if pkt, err = conn.ReadPacket(); err != nil {
				break
			}

			if playstream != nil {
				playstream.WritePacket(pkt)
			}

			fmt.Println("publish ", pkt.Time)
		}
	}

	server.ListenAndServe()

}
