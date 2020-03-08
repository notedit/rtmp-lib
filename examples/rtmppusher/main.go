package main

import (
	"os"

	rtmp "github.com/notedit/rtmp-lib"
	"github.com/notedit/rtmp-lib/flv"
)

func main() {

	file, err := os.Open("test.flv")
	if err != nil {
		panic(err)
	}

	conn, _ := rtmp.Dial("rtmp://localhost/app/publish")

	demuxer := flv.NewDemuxer(file)Z

	streams, err := demuxer.Streams()

	if err != nil {
		panic(err)
	}

	err = conn.WriteHeader(streams)

	if err != nil {
		panic(err)
	}

	for {

		packet, err := demuxer.ReadPacket()

		if err != nil {
			break
		}
		conn.WritePacket(packet)
	}

	conn.WriteTrailer()
}
