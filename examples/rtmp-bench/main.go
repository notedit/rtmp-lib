package main

import (
	"flag"
	"fmt"
	rtmp "github.com/notedit/rtmp-lib"
)

var (
	stream string
	number int
)

func pullstream(url string, index int) error {

	conn, _ := rtmp.Dial(url)

	_, err := conn.Streams()

	if err != nil {
		return err
	}

	go func() {
		for {
			_, err := conn.ReadPacket()
			if err != nil {
				break
			}
		}

		fmt.Printf("stream %d finished\n", index)
	}()

	return nil

}

func main() {

	flag.StringVar(&stream, "stream", "rtmp://localhost/live/live", "stream url")
	flag.IntVar(&number, "number", 100, "number stream to play")
	flag.Parse()

	for i := 0; i < number; i++ {
		pullstream(stream, i)
	}
}
