package flv

import (
	"bufio"
	"fmt"
	"io"
	"time"

	"github.com/notedit/rtmp-lib/aac"
	"github.com/notedit/rtmp-lib/av"
	"github.com/notedit/rtmp-lib/h264"
	"github.com/notedit/rtmp-lib/pio"
)

var MaxProbePacketCount = 20

func TsToTime(ts int32) time.Duration {
	return time.Millisecond * time.Duration(ts)
}

func TimeToTs(tm time.Duration) int32 {
	return int32(tm / time.Millisecond)
}

const MaxTagSubHeaderLength = 16

const (
	TAG_AUDIO      = 8
	TAG_VIDEO      = 9
	TAG_SCRIPTDATA = 18
)

const (
	SOUND_MP3                   = 2
	SOUND_NELLYMOSER_16KHZ_MONO = 4
	SOUND_NELLYMOSER_8KHZ_MONO  = 5
	SOUND_NELLYMOSER            = 6
	SOUND_ALAW                  = 7
	SOUND_MULAW                 = 8
	SOUND_AAC                   = 10
	SOUND_SPEEX                 = 11

	SOUND_5_5Khz = 0
	SOUND_11Khz  = 1
	SOUND_22Khz  = 2
	SOUND_44Khz  = 3

	SOUND_8BIT  = 0
	SOUND_16BIT = 1

	SOUND_MONO   = 0
	SOUND_STEREO = 1

	AAC_SEQHDR = 0
	AAC_RAW    = 1
)

const (
	AVC_SEQHDR = 0
	AVC_NALU   = 1
	AVC_EOS    = 2

	FRAME_KEY   = 1
	FRAME_INTER = 2

	VIDEO_H264 = 7
)

type Tag struct {
	Type uint8

	/*
		SoundFormat: UB[4]
		0 = Linear PCM, platform endian
		1 = ADPCM
		2 = MP3
		3 = Linear PCM, little endian
		4 = Nellymoser 16-kHz mono
		5 = Nellymoser 8-kHz mono
		6 = Nellymoser
		7 = G.711 A-law logarithmic PCM
		8 = G.711 mu-law logarithmic PCM
		9 = reserved
		10 = AAC
		11 = Speex
		14 = MP3 8-Khz
		15 = Device-specific sound
		Formats 7, 8, 14, and 15 are reserved for internal use
		AAC is supported in Flash Player 9,0,115,0 and higher.
		Speex is supported in Flash Player 10 and higher.
	*/
	SoundFormat uint8

	/*
		SoundRate: UB[2]
		Sampling rate
		0 = 5.5-kHz For AAC: always 3
		1 = 11-kHz
		2 = 22-kHz
		3 = 44-kHz
	*/
	SoundRate uint8

	/*
		SoundSize: UB[1]
		0 = snd8Bit
		1 = snd16Bit
		Size of each sample.
		This parameter only pertains to uncompressed formats.
		Compressed formats always decode to 16 bits internally
	*/
	SoundSize uint8

	/*
		SoundType: UB[1]
		0 = sndMono
		1 = sndStereo
		Mono or stereo sound For Nellymoser: always 0
		For AAC: always 1
	*/
	SoundType uint8

	/*
		0: AAC sequence header
		1: AAC raw
	*/
	AACPacketType uint8

	/*
		1: keyframe (for AVC, a seekable frame)
		2: inter frame (for AVC, a non- seekable frame)
		3: disposable inter frame (H.263 only)
		4: generated keyframe (reserved for server use only)
		5: video info/command frame
	*/
	FrameType uint8

	/*
		1: JPEG (currently unused)
		2: Sorenson H.263
		3: Screen video
		4: On2 VP6
		5: On2 VP6 with alpha channel
		6: Screen video version 2
		7: AVC
	*/
	CodecID uint8

	/*
		0: AVC sequence header
		1: AVC NALU
		2: AVC end of sequence (lower level NALU sequence ender is not required or supported)
	*/
	AVCPacketType uint8

	CompositionTime int32

	Data []byte
}

func (self Tag) ChannelLayout() av.ChannelLayout {
	if self.SoundType == SOUND_MONO {
		return av.CH_MONO
	} else {
		return av.CH_STEREO
	}
}

func (self *Tag) audioParseHeader(b []byte) (n int, err error) {
	if len(b) < n+1 {
		err = fmt.Errorf("audiodata: parse invalid")
		return
	}

	flags := b[n]
	n++
	self.SoundFormat = flags >> 4
	self.SoundRate = (flags >> 2) & 0x3
	self.SoundSize = (flags >> 1) & 0x1
	self.SoundType = flags & 0x1

	switch self.SoundFormat {
	case SOUND_AAC:
		if len(b) < n+1 {
			err = fmt.Errorf("audiodata: parse invalid")
			return
		}
		self.AACPacketType = b[n]
		n++
	}

	return
}

func (self Tag) audioFillHeader(b []byte) (n int) {
	var flags uint8
	flags |= self.SoundFormat << 4
	flags |= self.SoundRate << 2
	flags |= self.SoundSize << 1
	flags |= self.SoundType
	b[n] = flags
	n++

	switch self.SoundFormat {
	case SOUND_AAC:
		b[n] = self.AACPacketType
		n++
	}

	return
}

func (self *Tag) videoParseHeader(b []byte) (n int, err error) {
	if len(b) < n+1 {
		err = fmt.Errorf("videodata: parse invalid")
		return
	}
	flags := b[n]
	self.FrameType = flags >> 4
	self.CodecID = flags & 0xf
	n++

	if self.FrameType == FRAME_INTER || self.FrameType == FRAME_KEY {
		if len(b) < n+4 {
			err = fmt.Errorf("videodata: parse invalid")
			return
		}
		self.AVCPacketType = b[n]
		n++

		self.CompositionTime = pio.I24BE(b[n:])
		n += 3
	}

	return
}

func (self Tag) videoFillHeader(b []byte) (n int) {
	flags := self.FrameType<<4 | self.CodecID
	b[n] = flags
	n++
	b[n] = self.AVCPacketType
	n++
	pio.PutI24BE(b[n:], self.CompositionTime)
	n += 3
	return
}

func (self Tag) FillHeader(b []byte) (n int) {
	switch self.Type {
	case TAG_AUDIO:
		return self.audioFillHeader(b)

	case TAG_VIDEO:
		return self.videoFillHeader(b)
	}

	return
}

func (self *Tag) ParseHeader(b []byte) (n int, err error) {
	switch self.Type {
	case TAG_AUDIO:
		return self.audioParseHeader(b)

	case TAG_VIDEO:
		return self.videoParseHeader(b)
	}

	return
}

const (
	// TypeFlagsReserved UB[5]
	// TypeFlagsAudio    UB[1] Audio tags are present
	// TypeFlagsReserved UB[1] Must be 0
	// TypeFlagsVideo    UB[1] Video tags are present
	FILE_HAS_AUDIO = 0x4
	FILE_HAS_VIDEO = 0x1
)

const TagHeaderLength = 11
const TagTrailerLength = 4

func ParseTagHeader(b []byte) (tag Tag, ts int32, datalen int, err error) {
	tagtype := b[0]

	switch tagtype {
	case TAG_AUDIO, TAG_VIDEO, TAG_SCRIPTDATA:
		tag = Tag{Type: tagtype}

	default:
		err = fmt.Errorf("flv: ReadTag tagtype=%d invalid", tagtype)
		return
	}

	datalen = int(pio.U24BE(b[1:4]))

	var tslo uint32
	var tshi uint8
	tslo = pio.U24BE(b[4:7])
	tshi = b[7]
	ts = int32(tslo | uint32(tshi)<<24)

	return
}

func ReadTag(r io.Reader, b []byte) (tag Tag, ts int32, err error) {
	if _, err = io.ReadFull(r, b[:TagHeaderLength]); err != nil {
		return
	}
	var datalen int
	if tag, ts, datalen, err = ParseTagHeader(b); err != nil {
		return
	}

	data := make([]byte, datalen)
	if _, err = io.ReadFull(r, data); err != nil {
		return
	}

	var n int
	if n, err = (&tag).ParseHeader(data); err != nil {
		return
	}
	tag.Data = data[n:]

	if _, err = io.ReadFull(r, b[:4]); err != nil {
		return
	}
	return
}

func FillTagHeader(b []byte, tagtype uint8, datalen int, ts int32) (n int) {
	b[n] = tagtype
	n++
	pio.PutU24BE(b[n:], uint32(datalen))
	n += 3
	pio.PutU24BE(b[n:], uint32(ts&0xffffff))
	n += 3
	b[n] = uint8(ts >> 24)
	n++
	pio.PutI24BE(b[n:], 0)
	n += 3
	return
}

func FillTagTrailer(b []byte, datalen int) (n int) {
	pio.PutU32BE(b[n:], uint32(datalen+TagHeaderLength))
	n += 4
	return
}

func WriteTag(w io.Writer, tag Tag, ts int32, b []byte) (err error) {
	data := tag.Data

	n := tag.FillHeader(b[TagHeaderLength:])
	datalen := len(data) + n

	n += FillTagHeader(b, tag.Type, datalen, ts)

	if _, err = w.Write(b[:n]); err != nil {
		return
	}

	if _, err = w.Write(data); err != nil {
		return
	}

	n = FillTagTrailer(b, datalen)
	if _, err = w.Write(b[:n]); err != nil {
		return
	}

	return
}

const FileHeaderLength = 9

func FillFileHeader(b []byte, flags uint8) (n int) {
	// 'FLV', version 1
	pio.PutU32BE(b[n:], 0x464c5601)
	n += 4

	b[n] = flags
	n++

	// DataOffset: UI32 Offset in bytes from start of file to start of body (that is, size of header)
	// The DataOffset field usually has a value of 9 for FLV version 1.
	pio.PutU32BE(b[n:], 9)
	n += 4

	// PreviousTagSize0: UI32 Always 0
	pio.PutU32BE(b[n:], 0)
	n += 4

	return
}

func ParseFileHeader(b []byte) (flags uint8, skip int, err error) {
	flv := pio.U24BE(b[0:3])
	if flv != 0x464c56 { // 'FLV'
		err = fmt.Errorf("flv: file header cc3 invalid")
		return
	}

	flags = b[4]

	skip = int(pio.U32BE(b[5:9])) - 9 + 4
	if skip < 0 {
		err = fmt.Errorf("flv: file header datasize invalid")
		return
	}

	return
}

func NewMetadataByStreams(streams []av.CodecData) (metadata AMFMap, err error) {
	metadata = AMFMap{}

	for _, _stream := range streams {
		typ := _stream.Type()
		switch {
		case typ.IsVideo():
			stream := _stream.(av.VideoCodecData)
			switch typ {
			case av.H264:
				metadata["videocodecid"] = VIDEO_H264

			default:
				err = fmt.Errorf("flv: metadata: unsupported video codecType=%v", stream.Type())
				return
			}

			metadata["width"] = stream.Width()
			metadata["height"] = stream.Height()
			metadata["displayWidth"] = stream.Width()
			metadata["displayHeight"] = stream.Height()

		case typ.IsAudio():
			stream := _stream.(av.AudioCodecData)
			switch typ {
			case av.AAC:
				metadata["audiocodecid"] = SOUND_AAC

			case av.SPEEX:
				metadata["audiocodecid"] = SOUND_SPEEX

			default:
				err = fmt.Errorf("flv: metadata: unsupported audio codecType=%v", stream.Type())
				return
			}

			metadata["audiosamplerate"] = stream.SampleRate()
		}
	}

	return
}

type Prober struct {
	HasAudio, HasVideo             bool
	GotAudio, GotVideo             bool
	VideoStreamIdx, AudioStreamIdx int
	PushedCount                    int
	Streams                        []av.CodecData
	CachedPkts                     []av.Packet
}

func (self *Prober) CacheTag(_tag Tag, timestamp int32) {
	pkt, _ := self.TagToPacket(_tag, timestamp)
	self.CachedPkts = append(self.CachedPkts, pkt)
}

func (self *Prober) PushTag(tag Tag, timestamp int32) (err error) {
	self.PushedCount++

	if self.PushedCount > MaxProbePacketCount {
		err = fmt.Errorf("flv: max probe packet count reached")
		return
	}

	switch tag.Type {
	case TAG_VIDEO:
		switch tag.AVCPacketType {
		case AVC_SEQHDR:
			if !self.GotVideo {
				var stream h264.CodecData
				if stream, err = h264.NewCodecDataFromAVCDecoderConfRecord(tag.Data); err != nil {
					err = fmt.Errorf("flv: h264 seqhdr invalid")
					return
				}
				self.VideoStreamIdx = len(self.Streams)
				self.Streams = append(self.Streams, stream)
				self.GotVideo = true
			}

		case AVC_NALU:
			self.CacheTag(tag, timestamp)
		}

	case TAG_AUDIO:
		switch tag.SoundFormat {
		case SOUND_AAC:
			switch tag.AACPacketType {
			case AAC_SEQHDR:
				if !self.GotAudio {
					var stream aac.CodecData
					if stream, err = aac.NewCodecDataFromMPEG4AudioConfigBytes(tag.Data); err != nil {
						err = fmt.Errorf("flv: aac seqhdr invalid")
						return
					}
					self.AudioStreamIdx = len(self.Streams)
					self.Streams = append(self.Streams, stream)
					self.GotAudio = true
				}

			case AAC_RAW:
				self.CacheTag(tag, timestamp)
			}
		}
	}

	return
}

func (self *Prober) Probed() (ok bool) {
	if self.HasAudio || self.HasVideo {
		if self.HasAudio == self.GotAudio && self.HasVideo == self.GotVideo {
			return true
		}
	} else {
		if self.PushedCount == MaxProbePacketCount {
			return true
		}
	}
	return
}

func (self *Prober) TagToPacket(tag Tag, timestamp int32) (pkt av.Packet, ok bool) {
	switch tag.Type {
	case TAG_VIDEO:
		pkt.Idx = int8(self.VideoStreamIdx)
		switch tag.AVCPacketType {
		case AVC_NALU:
			ok = true
			pkt.Data = tag.Data
			pkt.CompositionTime = TsToTime(tag.CompositionTime)
			pkt.IsKeyFrame = tag.FrameType == FRAME_KEY
		}

	case TAG_AUDIO:
		pkt.Idx = int8(self.AudioStreamIdx)
		switch tag.SoundFormat {
		case SOUND_AAC:
			switch tag.AACPacketType {
			case AAC_RAW:
				ok = true
				pkt.Data = tag.Data
			}

		case SOUND_SPEEX:
			ok = true
			pkt.Data = tag.Data

		case SOUND_NELLYMOSER:
			ok = true
			pkt.Data = tag.Data
		}
	}

	pkt.Time = TsToTime(timestamp)
	return
}

func (self *Prober) Empty() bool {
	return len(self.CachedPkts) == 0
}

func (self *Prober) PopPacket() av.Packet {
	pkt := self.CachedPkts[0]
	self.CachedPkts = self.CachedPkts[1:]
	return pkt
}

func CodecDataToTag(stream av.CodecData) (_tag Tag, ok bool, err error) {
	switch stream.Type() {
	case av.H264:
		codec := stream.(h264.CodecData)
		tag := Tag{
			Type:          TAG_VIDEO,
			AVCPacketType: AVC_SEQHDR,
			CodecID:       VIDEO_H264,
			Data:          codec.AVCDecoderConfRecordBytes(),
			FrameType:     FRAME_KEY,
		}
		ok = true
		_tag = tag

	case av.NELLYMOSER:
	case av.SPEEX:

	case av.AAC:
		codec := stream.(aac.CodecData)
		tag := Tag{
			Type:          TAG_AUDIO,
			SoundFormat:   SOUND_AAC,
			SoundRate:     SOUND_44Khz,
			AACPacketType: AAC_SEQHDR,
			Data:          codec.MPEG4AudioConfigBytes(),
		}
		switch codec.SampleFormat().BytesPerSample() {
		case 1:
			tag.SoundSize = SOUND_8BIT
		default:
			tag.SoundSize = SOUND_16BIT
		}
		switch codec.ChannelLayout().Count() {
		case 1:
			tag.SoundType = SOUND_MONO
		case 2:
			tag.SoundType = SOUND_STEREO
		}
		ok = true
		_tag = tag

	default:
		err = fmt.Errorf("flv: unspported codecType=%v", stream.Type())
		return
	}
	return
}

func PacketToTag(pkt av.Packet, stream av.CodecData) (tag Tag, timestamp int32) {
	switch stream.Type() {
	case av.H264:
		tag = Tag{
			Type:            TAG_VIDEO,
			AVCPacketType:   AVC_NALU,
			CodecID:         VIDEO_H264,
			Data:            pkt.Data,
			CompositionTime: TimeToTs(pkt.CompositionTime),
		}
		if pkt.IsKeyFrame {
			tag.FrameType = FRAME_KEY
		} else {
			tag.FrameType = FRAME_INTER
		}

	case av.AAC:
		tag = Tag{
			Type:          TAG_AUDIO,
			SoundFormat:   SOUND_AAC,
			SoundRate:     SOUND_44Khz,
			AACPacketType: AAC_RAW,
			Data:          pkt.Data,
		}
		astream := stream.(av.AudioCodecData)
		switch astream.SampleFormat().BytesPerSample() {
		case 1:
			tag.SoundSize = SOUND_8BIT
		default:
			tag.SoundSize = SOUND_16BIT
		}
		switch astream.ChannelLayout().Count() {
		case 1:
			tag.SoundType = SOUND_MONO
		case 2:
			tag.SoundType = SOUND_STEREO
		}

	case av.SPEEX:
		tag = Tag{
			Type:        TAG_AUDIO,
			SoundFormat: SOUND_SPEEX,
			Data:        pkt.Data,
		}

	case av.NELLYMOSER:
		tag = Tag{
			Type:        TAG_AUDIO,
			SoundFormat: SOUND_NELLYMOSER,
			Data:        pkt.Data,
		}
	}

	timestamp = TimeToTs(pkt.Time)
	return
}

type Muxer struct {
	bufw    writeFlusher
	b       []byte
	streams []av.CodecData
}

type writeFlusher interface {
	io.Writer
	Flush() error
}

func NewMuxerWriteFlusher(w writeFlusher) *Muxer {
	return &Muxer{
		bufw: w,
		b:    make([]byte, 256),
	}
}

func NewMuxer(w io.Writer) *Muxer {
	return NewMuxerWriteFlusher(bufio.NewWriterSize(w, 1024*64))
}

var CodecTypes = []av.CodecType{av.H264, av.AAC, av.SPEEX}

func (self *Muxer) WriteHeader(streams []av.CodecData) (err error) {
	var flags uint8
	for _, stream := range streams {
		if stream.Type().IsVideo() {
			flags |= FILE_HAS_VIDEO
		} else if stream.Type().IsAudio() {
			flags |= FILE_HAS_AUDIO
		}
	}

	n := FillFileHeader(self.b, flags)
	if _, err = self.bufw.Write(self.b[:n]); err != nil {
		return
	}

	for _, stream := range streams {
		var tag Tag
		var ok bool
		if tag, ok, err = CodecDataToTag(stream); err != nil {
			return
		}
		if ok {
			if err = WriteTag(self.bufw, tag, 0, self.b); err != nil {
				return
			}
		}
	}

	self.streams = streams
	return
}

func (self *Muxer) WritePacket(pkt av.Packet) (err error) {
	stream := self.streams[pkt.Idx]
	tag, timestamp := PacketToTag(pkt, stream)

	if err = WriteTag(self.bufw, tag, timestamp, self.b); err != nil {
		return
	}
	return
}

func (self *Muxer) WriteTrailer() (err error) {
	if err = self.bufw.Flush(); err != nil {
		return
	}
	return
}

type Demuxer struct {
	prober *Prober
	bufr   *bufio.Reader
	b      []byte
	stage  int
}

func NewDemuxer(r io.Reader) *Demuxer {
	return &Demuxer{
		bufr:   bufio.NewReaderSize(r, 1024*10),
		prober: &Prober{},
		b:      make([]byte, 256),
	}
}

func (self *Demuxer) prepare() (err error) {
	for self.stage < 2 {
		switch self.stage {
		case 0:
			if _, err = io.ReadFull(self.bufr, self.b[:FileHeaderLength]); err != nil {
				return
			}
			var flags uint8
			var skip int
			if flags, skip, err = ParseFileHeader(self.b); err != nil {
				return
			}
			if _, err = self.bufr.Discard(skip); err != nil {
				return
			}
			if flags&FILE_HAS_AUDIO != 0 {
				self.prober.HasAudio = true
			}
			if flags&FILE_HAS_VIDEO != 0 {
				self.prober.HasVideo = true
			}
			self.stage++

		case 1:
			for !self.prober.Probed() {
				var tag Tag
				var timestamp int32
				if tag, timestamp, err = ReadTag(self.bufr, self.b); err != nil {
					return
				}
				if err = self.prober.PushTag(tag, timestamp); err != nil {
					return
				}
			}
			self.stage++
		}
	}
	return
}

func (self *Demuxer) Streams() (streams []av.CodecData, err error) {
	if err = self.prepare(); err != nil {
		return
	}
	streams = self.prober.Streams
	return
}

func (self *Demuxer) ReadPacket() (pkt av.Packet, err error) {
	if err = self.prepare(); err != nil {
		return
	}

	if !self.prober.Empty() {
		pkt = self.prober.PopPacket()
		return
	}

	for {
		var tag Tag
		var timestamp int32
		if tag, timestamp, err = ReadTag(self.bufr, self.b); err != nil {
			return
		}

		var ok bool
		if pkt, ok = self.prober.TagToPacket(tag, timestamp); ok {
			return
		}
	}

	return
}
