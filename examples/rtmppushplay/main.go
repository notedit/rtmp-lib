package main

import (
	rtmp "github.com/notedit/rtmp-lib"
)

func main() {

	server := &rtmp.Server{}

	var publisherConn *rtmp.Conn

	server.HandlePlay = func(conn *rtmp.Conn) {

		streams, err := publisherConn.Streams()

		if err != nil {
			panic(err)
		}

		conn.WriteHeader(streams)

		for {
			packet, err := publisherConn.ReadPacket()
			if err != nil {
				break
			}
			conn.WritePacket(packet)
		}
	}

	server.HandlePublish = func(conn *rtmp.Conn) {

		publisherConn = conn

	}

	server.ListenAndServe()

}
