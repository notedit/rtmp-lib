package audio

/*
#cgo LDFLAGS: -lavformat -lavutil -lavcodec -lavresample
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/avutil.h>
#include <libavresample/avresample.h>
#include <libavutil/opt.h>
#include <string.h>

typedef struct {
	AVCodec *codec;
	AVCodecContext *codecCtx;
	AVFrame *frame;
	AVDictionary *options;
	int profile;
} FFCtx;

static inline int avcodec_profile_name_to_int(AVCodec *codec, const char *name) {
	const AVProfile *p;
	for (p = codec->profiles; p != NULL && p->profile != FF_PROFILE_UNKNOWN; p++)
		if (!strcasecmp(p->name, name))
			return p->profile;
	return FF_PROFILE_UNKNOWN;
}

int wrap_avcodec_decode_audio4(AVCodecContext *ctx, AVFrame *frame, void *data, int size, int *got) {
	struct AVPacket pkt = {.data = data, .size = size};
	return avcodec_decode_audio4(ctx, frame, got, &pkt);
}
int wrap_avresample_convert(AVAudioResampleContext *avr, int *out, int outsize, int outcount, int *in, int insize, int incount) {
	return avresample_convert(avr, (void *)out, outsize, outcount, (void *)in, insize, incount);
}

void ffinit() {
	av_register_all();
}
*/
import "C"

import (
	"fmt"
	"github.com/notedit/rtmp-lib/aac"
	"github.com/notedit/rtmp-lib/av"
	"runtime"
	"time"
	"unsafe"
)

func init() {
	C.ffinit()
}

type ffctx struct {
	ff C.FFCtx
}

type Resampler struct {
	inSampleFormat, OutSampleFormat   av.SampleFormat
	inChannelLayout, OutChannelLayout av.ChannelLayout
	inSampleRate, OutSampleRate       int
	avr                               *C.AVAudioResampleContext
}

func (self *Resampler) Resample(in av.AudioFrame) (out av.AudioFrame, err error) {
	formatChange := in.SampleRate != self.inSampleRate || in.SampleFormat != self.inSampleFormat || in.ChannelLayout != self.inChannelLayout

	var flush av.AudioFrame

	if formatChange {

		self.inSampleFormat = in.SampleFormat
		self.inSampleRate = in.SampleRate
		self.inChannelLayout = in.ChannelLayout
		avr := C.avresample_alloc_context()
		C.av_opt_set_int(unsafe.Pointer(avr), C.CString("in_channel_layout"), C.int64_t(channelLayoutAV2FF(self.inChannelLayout)), 0)
		C.av_opt_set_int(unsafe.Pointer(avr), C.CString("out_channel_layout"), C.int64_t(channelLayoutAV2FF(self.OutChannelLayout)), 0)
		C.av_opt_set_int(unsafe.Pointer(avr), C.CString("in_sample_rate"), C.int64_t(self.inSampleRate), 0)
		C.av_opt_set_int(unsafe.Pointer(avr), C.CString("out_sample_rate"), C.int64_t(self.OutSampleRate), 0)
		C.av_opt_set_int(unsafe.Pointer(avr), C.CString("in_sample_fmt"), C.int64_t(sampleFormatAV2FF(self.inSampleFormat)), 0)
		C.av_opt_set_int(unsafe.Pointer(avr), C.CString("out_sample_fmt"), C.int64_t(sampleFormatAV2FF(self.OutSampleFormat)), 0)
		C.avresample_open(avr)
		self.avr = avr
	}

	var inChannels, inLinesize int
	inSampleCount := in.SampleCount
	if !self.inSampleFormat.IsPlanar() {
		inChannels = 1
		inLinesize = inSampleCount * in.SampleFormat.BytesPerSample() * self.inChannelLayout.Count()
	} else {
		inChannels = self.inChannelLayout.Count()
		inLinesize = inSampleCount * in.SampleFormat.BytesPerSample()
	}
	inData := make([]*C.uint8_t, inChannels)
	for i := 0; i < inChannels; i++ {
		inData[i] = (*C.uint8_t)(unsafe.Pointer(&in.Data[i][0]))
	}

	var outChannels, outLinesize, outBytesPerSample int
	outSampleCount := int(C.avresample_get_out_samples(self.avr, C.int(in.SampleCount)))
	if !self.OutSampleFormat.IsPlanar() {
		outChannels = 1
		outBytesPerSample = self.OutSampleFormat.BytesPerSample() * self.OutChannelLayout.Count()
		outLinesize = outSampleCount * outBytesPerSample
	} else {
		outChannels = self.OutChannelLayout.Count()
		outBytesPerSample = self.OutSampleFormat.BytesPerSample()
		outLinesize = outSampleCount * outBytesPerSample
	}
	outData := make([]*C.uint8_t, outChannels)
	out.Data = make([][]byte, outChannels)
	for i := 0; i < outChannels; i++ {
		out.Data[i] = make([]byte, outLinesize)
		outData[i] = (*C.uint8_t)(unsafe.Pointer(&out.Data[i][0]))
	}
	out.ChannelLayout = self.OutChannelLayout
	out.SampleFormat = self.OutSampleFormat
	out.SampleRate = self.OutSampleRate

	convertSamples := int(C.wrap_avresample_convert(
		self.avr,
		(*C.int)(unsafe.Pointer(&outData[0])), C.int(outLinesize), C.int(outSampleCount),
		(*C.int)(unsafe.Pointer(&inData[0])), C.int(inLinesize), C.int(inSampleCount),
	))
	if convertSamples < 0 {
		err = fmt.Errorf("ffmpeg: avresample_convert_frame failed")
		return
	}

	out.SampleCount = convertSamples
	if convertSamples < outSampleCount {
		for i := 0; i < outChannels; i++ {
			out.Data[i] = out.Data[i][:convertSamples*outBytesPerSample]
		}
	}

	if flush.SampleCount > 0 {
		out = flush.Concat(out)
	}

	return
}

func (self *Resampler) Close() {
	C.avresample_free(&self.avr)
}

func newFFCtxByCodec(codec *C.AVCodec) (ff *ffctx, err error) {
	ff = &ffctx{}
	ff.ff.codec = codec
	ff.ff.codecCtx = C.avcodec_alloc_context3(codec)
	ff.ff.profile = C.FF_PROFILE_UNKNOWN
	runtime.SetFinalizer(ff, freeFFCtx)
	return
}

func freeFFCtx(self *ffctx) {
	ff := &self.ff
	if ff.frame != nil {
		C.av_frame_free(&ff.frame)
	}
	if ff.codecCtx != nil {
		C.avcodec_close(ff.codecCtx)
		C.av_free(unsafe.Pointer(ff.codecCtx))
		ff.codecCtx = nil
	}
	if ff.options != nil {
		C.av_dict_free(&ff.options)
	}
}

type AudioEncoder struct {
	ff               *ffctx
	SampleRate       int
	Bitrate          int
	ChannelLayout    av.ChannelLayout
	SampleFormat     av.SampleFormat
	FrameSampleCount int
	framebuf         av.AudioFrame
	codecData        av.AudioCodecData
	resampler        *Resampler
}

func sampleFormatAV2FF(sampleFormat av.SampleFormat) (ffsamplefmt int32) {
	switch sampleFormat {
	case av.U8:
		ffsamplefmt = C.AV_SAMPLE_FMT_U8
	case av.S16:
		ffsamplefmt = C.AV_SAMPLE_FMT_S16
	case av.S32:
		ffsamplefmt = C.AV_SAMPLE_FMT_S32
	case av.FLT:
		ffsamplefmt = C.AV_SAMPLE_FMT_FLT
	case av.DBL:
		ffsamplefmt = C.AV_SAMPLE_FMT_DBL
	case av.U8P:
		ffsamplefmt = C.AV_SAMPLE_FMT_U8P
	case av.S16P:
		ffsamplefmt = C.AV_SAMPLE_FMT_S16P
	case av.S32P:
		ffsamplefmt = C.AV_SAMPLE_FMT_S32P
	case av.FLTP:
		ffsamplefmt = C.AV_SAMPLE_FMT_FLTP
	case av.DBLP:
		ffsamplefmt = C.AV_SAMPLE_FMT_DBLP
	}
	return
}

func sampleFormatFF2AV(ffsamplefmt int32) (sampleFormat av.SampleFormat) {
	switch ffsamplefmt {
	case C.AV_SAMPLE_FMT_U8: ///< unsigned 8 bits
		sampleFormat = av.U8
	case C.AV_SAMPLE_FMT_S16: ///< signed 16 bits
		sampleFormat = av.S16
	case C.AV_SAMPLE_FMT_S32: ///< signed 32 bits
		sampleFormat = av.S32
	case C.AV_SAMPLE_FMT_FLT: ///< float
		sampleFormat = av.FLT
	case C.AV_SAMPLE_FMT_DBL: ///< double
		sampleFormat = av.DBL
	case C.AV_SAMPLE_FMT_U8P: ///< unsigned 8 bits, planar
		sampleFormat = av.U8P
	case C.AV_SAMPLE_FMT_S16P: ///< signed 16 bits, planar
		sampleFormat = av.S16P
	case C.AV_SAMPLE_FMT_S32P: ///< signed 32 bits, planar
		sampleFormat = av.S32P
	case C.AV_SAMPLE_FMT_FLTP: ///< float, planar
		sampleFormat = av.FLTP
	case C.AV_SAMPLE_FMT_DBLP: ///< double, planar
		sampleFormat = av.DBLP
	}
	return
}

func (self *AudioEncoder) SetSampleFormat(fmt av.SampleFormat) (err error) {
	self.SampleFormat = fmt
	return
}

func (self *AudioEncoder) SetSampleRate(rate int) (err error) {
	self.SampleRate = rate
	return
}

func (self *AudioEncoder) SetChannelLayout(ch av.ChannelLayout) (err error) {
	self.ChannelLayout = ch
	return
}

func (self *AudioEncoder) SetBitrate(bitrate int) (err error) {
	self.Bitrate = bitrate
	return
}

func (self *AudioEncoder) SetOption(key string, val interface{}) (err error) {
	ff := &self.ff.ff

	sval := fmt.Sprint(val)
	if key == "profile" {
		ff.profile = C.avcodec_profile_name_to_int(ff.codec, C.CString(sval))
		if ff.profile == C.FF_PROFILE_UNKNOWN {
			err = fmt.Errorf("ffmpeg: profile `%s` invalid", sval)
			return
		}
		return
	}

	C.av_dict_set(&ff.options, C.CString(key), C.CString(sval), 0)
	return
}

func (self *AudioEncoder) GetOption(key string, val interface{}) (err error) {
	ff := &self.ff.ff
	entry := C.av_dict_get(ff.options, C.CString(key), nil, 0)
	if entry == nil {
		err = fmt.Errorf("ffmpeg: GetOption failed: `%s` not exists", key)
		return
	}
	switch p := val.(type) {
	case *string:
		*p = C.GoString(entry.value)
	case *int:
		fmt.Sscanf(C.GoString(entry.value), "%d", p)
	default:
		err = fmt.Errorf("ffmpeg: GetOption failed: val must be *string or *int receiver")
		return
	}
	return
}

func (self *AudioEncoder) Setup() (err error) {
	ff := &self.ff.ff

	if self.SampleFormat == av.SampleFormat(0) {
		self.SampleFormat = sampleFormatFF2AV(*ff.codec.sample_fmts)
	}

	if self.SampleRate == 0 {
		self.SampleRate = 44100
	}
	if self.ChannelLayout == av.ChannelLayout(0) {
		self.ChannelLayout = av.CH_STEREO
	}

	ff.codecCtx.sample_fmt = sampleFormatAV2FF(self.SampleFormat)
	ff.codecCtx.sample_rate = C.int(self.SampleRate)
	ff.codecCtx.bit_rate = C.int64_t(self.Bitrate)
	ff.codecCtx.channel_layout = channelLayoutAV2FF(self.ChannelLayout)
	//ff.codecCtx.strict_std_compliance = C.FF_COMPLIANCE_EXPERIMENTAL
	//ff.codecCtx.flags = C.AV_CODEC_FLAG_GLOBAL_HEADER
	//ff.codecCtx.profile = ff.profile

	if C.avcodec_open2(ff.codecCtx, ff.codec, nil) != 0 {
		err = fmt.Errorf("ffmpeg: encoder: avcodec_open2 failed")
		return
	}
	self.SampleFormat = sampleFormatFF2AV(ff.codecCtx.sample_fmt)
	self.FrameSampleCount = int(ff.codecCtx.frame_size)

	extradata := C.GoBytes(unsafe.Pointer(ff.codecCtx.extradata), ff.codecCtx.extradata_size)

	ff.frame = C.av_frame_alloc()

	switch ff.codecCtx.codec_id {
	case C.AV_CODEC_ID_AAC:
		if self.codecData, err = aac.NewCodecDataFromMPEG4AudioConfigBytes(extradata); err != nil {
			return
		}

	default:
		self.codecData = audioCodecData{
			channelLayout: self.ChannelLayout,
			sampleFormat:  self.SampleFormat,
			sampleRate:    self.SampleRate,
			codecId:       ff.codecCtx.codec_id,
			extradata:     extradata,
		}
	}

	return
}

func (self *AudioEncoder) prepare() (err error) {
	ff := &self.ff.ff

	if ff.frame == nil {
		if err = self.Setup(); err != nil {
			return
		}
	}

	return
}

func (self *AudioEncoder) CodecData() (codec av.AudioCodecData, err error) {
	if err = self.prepare(); err != nil {
		return
	}
	codec = self.codecData
	return
}

func (self *AudioEncoder) encodeOne(frame av.AudioFrame) (gotpkt bool, pkt []byte, err error) {
	if err = self.prepare(); err != nil {
		return
	}

	ff := &self.ff.ff

	cpkt := C.AVPacket{}
	cgotpkt := C.int(0)
	audioFrameAssignToFF(frame, ff.frame)

	fmt.Println("frame length", len(frame.Data[0]))

	cerr := C.avcodec_encode_audio2(ff.codecCtx, &cpkt, ff.frame, &cgotpkt)
	if cerr < C.int(0) {
		err = fmt.Errorf("ffmpeg: avcodec_encode_audio2 failed: %d", cerr)
		return
	}

	if cgotpkt != 0 {
		gotpkt = true
		pkt = C.GoBytes(unsafe.Pointer(cpkt.data), cpkt.size)
		C.av_packet_unref(&cpkt)

	}

	return
}

func (self *AudioEncoder) resample(in av.AudioFrame) (out av.AudioFrame, err error) {
	if self.resampler == nil {
		self.resampler = &Resampler{
			OutSampleFormat:  self.SampleFormat,
			OutSampleRate:    self.SampleRate,
			OutChannelLayout: self.ChannelLayout,
		}
	}
	if out, err = self.resampler.Resample(in); err != nil {
		return
	}
	return
}

func (self *AudioEncoder) Encode(frame av.AudioFrame) (pkts [][]byte, err error) {
	var gotpkt bool
	var pkt []byte

	if frame.SampleFormat != self.SampleFormat || frame.ChannelLayout != self.ChannelLayout || frame.SampleRate != self.SampleRate {
		if frame, err = self.resample(frame); err != nil {
			return
		}
	}

	if self.FrameSampleCount != 0 {
		if self.framebuf.SampleCount == 0 {
			self.framebuf = frame
		} else {
			self.framebuf = self.framebuf.Concat(frame)
		}
		for self.framebuf.SampleCount >= self.FrameSampleCount {
			frame := self.framebuf.Slice(0, self.FrameSampleCount)
			if gotpkt, pkt, err = self.encodeOne(frame); err != nil {
				return
			}
			if gotpkt {
				pkts = append(pkts, pkt)
			}
			self.framebuf = self.framebuf.Slice(self.FrameSampleCount, self.framebuf.SampleCount)
		}
	} else {
		if gotpkt, pkt, err = self.encodeOne(frame); err != nil {
			return
		}
		if gotpkt {
			pkts = append(pkts, pkt)
		}
	}

	return
}

func (self *AudioEncoder) PacketDuration(data []byte) (dur time.Duration, err error) {
	ff := &self.ff.ff
	duration := C.av_get_audio_frame_duration(ff.codecCtx, C.int(len(data)))
	dur = time.Duration(int(duration)) * time.Second / time.Duration(self.SampleRate)
	return
}

func (self *AudioEncoder) Close() {
	freeFFCtx(self.ff)
	if self.resampler != nil {
		self.resampler.Close()
		self.resampler = nil
	}
}

func audioFrameAssignToAVParams(f *C.AVFrame, frame *av.AudioFrame) {
	frame.SampleFormat = sampleFormatFF2AV(int32(f.format))
	frame.ChannelLayout = channelLayoutFF2AV(f.channel_layout)
	frame.SampleRate = int(f.sample_rate)
}

func audioFrameAssignToAVData(f *C.AVFrame, frame *av.AudioFrame) {
	frame.SampleCount = int(f.nb_samples)
	frame.Data = make([][]byte, int(f.channels))
	for i := 0; i < int(f.channels); i++ {
		frame.Data[i] = C.GoBytes(unsafe.Pointer(f.data[i]), f.linesize[0])
	}
}

func audioFrameAssignToAV(f *C.AVFrame, frame *av.AudioFrame) {
	audioFrameAssignToAVParams(f, frame)
	audioFrameAssignToAVData(f, frame)
}

func audioFrameAssignToFFParams(frame av.AudioFrame, f *C.AVFrame) {
	f.format = C.int(sampleFormatAV2FF(frame.SampleFormat))
	f.channel_layout = channelLayoutAV2FF(frame.ChannelLayout)
	f.sample_rate = C.int(frame.SampleRate)
	f.channels = C.int(frame.ChannelLayout.Count())
}

func audioFrameAssignToFFData(frame av.AudioFrame, f *C.AVFrame) {
	f.nb_samples = C.int(frame.SampleCount)
	for i := range frame.Data {
		f.data[i] = (*C.uint8_t)(unsafe.Pointer(&frame.Data[i][0]))
		f.linesize[i] = C.int(len(frame.Data[i]))
	}
}

func audioFrameAssignToFF(frame av.AudioFrame, f *C.AVFrame) {
	audioFrameAssignToFFParams(frame, f)
	audioFrameAssignToFFData(frame, f)
}

func channelLayoutFF2AV(layout C.uint64_t) (channelLayout av.ChannelLayout) {
	if layout&C.AV_CH_FRONT_CENTER != 0 {
		channelLayout |= av.CH_FRONT_CENTER
	}
	if layout&C.AV_CH_FRONT_LEFT != 0 {
		channelLayout |= av.CH_FRONT_LEFT
	}
	if layout&C.AV_CH_FRONT_RIGHT != 0 {
		channelLayout |= av.CH_FRONT_RIGHT
	}
	if layout&C.AV_CH_BACK_CENTER != 0 {
		channelLayout |= av.CH_BACK_CENTER
	}
	if layout&C.AV_CH_BACK_LEFT != 0 {
		channelLayout |= av.CH_BACK_LEFT
	}
	if layout&C.AV_CH_BACK_RIGHT != 0 {
		channelLayout |= av.CH_BACK_RIGHT
	}
	if layout&C.AV_CH_SIDE_LEFT != 0 {
		channelLayout |= av.CH_SIDE_LEFT
	}
	if layout&C.AV_CH_SIDE_RIGHT != 0 {
		channelLayout |= av.CH_SIDE_RIGHT
	}
	if layout&C.AV_CH_LOW_FREQUENCY != 0 {
		channelLayout |= av.CH_LOW_FREQ
	}
	return
}

func channelLayoutAV2FF(channelLayout av.ChannelLayout) (layout C.uint64_t) {
	if channelLayout&av.CH_FRONT_CENTER != 0 {
		layout |= C.AV_CH_FRONT_CENTER
	}
	if channelLayout&av.CH_FRONT_LEFT != 0 {
		layout |= C.AV_CH_FRONT_LEFT
	}
	if channelLayout&av.CH_FRONT_RIGHT != 0 {
		layout |= C.AV_CH_FRONT_RIGHT
	}
	if channelLayout&av.CH_BACK_CENTER != 0 {
		layout |= C.AV_CH_BACK_CENTER
	}
	if channelLayout&av.CH_BACK_LEFT != 0 {
		layout |= C.AV_CH_BACK_LEFT
	}
	if channelLayout&av.CH_BACK_RIGHT != 0 {
		layout |= C.AV_CH_BACK_RIGHT
	}
	if channelLayout&av.CH_SIDE_LEFT != 0 {
		layout |= C.AV_CH_SIDE_LEFT
	}
	if channelLayout&av.CH_SIDE_RIGHT != 0 {
		layout |= C.AV_CH_SIDE_RIGHT
	}
	if channelLayout&av.CH_LOW_FREQ != 0 {
		layout |= C.AV_CH_LOW_FREQUENCY
	}
	return
}

type AudioDecoder struct {
	ff            *ffctx
	ChannelLayout av.ChannelLayout
	SampleFormat  av.SampleFormat
	SampleRate    int
	Extradata     []byte
}

func (self *AudioDecoder) SetSampleFormat(fmt av.SampleFormat) (err error) {
	self.SampleFormat = fmt
	return
}

func (self *AudioDecoder) SetSampleRate(rate int) (err error) {
	self.SampleRate = rate
	return
}

func (self *AudioDecoder) SetChannelLayout(ch av.ChannelLayout) (err error) {
	self.ChannelLayout = ch
	return
}

func (self *AudioDecoder) Setup() (err error) {
	ff := &self.ff.ff

	ff.frame = C.av_frame_alloc()

	if len(self.Extradata) > 0 {
		ff.codecCtx.extradata = (*C.uint8_t)(unsafe.Pointer(&self.Extradata[0]))
		ff.codecCtx.extradata_size = C.int(len(self.Extradata))
	}

	ff.codecCtx.sample_rate = C.int(self.SampleRate)
	ff.codecCtx.channel_layout = channelLayoutAV2FF(self.ChannelLayout)
	ff.codecCtx.channels = C.int(self.ChannelLayout.Count())
	if C.avcodec_open2(ff.codecCtx, ff.codec, nil) != 0 {
		err = fmt.Errorf("ffmpeg: decoder: avcodec_open2 failed")
		return
	}
	self.SampleFormat = sampleFormatFF2AV(ff.codecCtx.sample_fmt)
	self.ChannelLayout = channelLayoutFF2AV(ff.codecCtx.channel_layout)
	if self.SampleRate == 0 {
		self.SampleRate = int(ff.codecCtx.sample_rate)
	}

	return
}

func (self *AudioDecoder) Decode(pkt []byte) (gotframe bool, frame av.AudioFrame, err error) {
	ff := &self.ff.ff

	cgotframe := C.int(0)
	cerr := C.wrap_avcodec_decode_audio4(ff.codecCtx, ff.frame, unsafe.Pointer(&pkt[0]), C.int(len(pkt)), &cgotframe)
	if cerr < C.int(0) {
		err = fmt.Errorf("ffmpeg: avcodec_decode_audio4 failed: %d", cerr)
		return
	}

	if cgotframe != C.int(0) {
		gotframe = true
		audioFrameAssignToAV(ff.frame, &frame)
		frame.SampleRate = self.SampleRate

	}

	return
}

func (self *AudioDecoder) PacketDuration(data []byte) (dur time.Duration, err error) {
	ff := &self.ff.ff
	duration := C.av_get_audio_frame_duration(ff.codecCtx, C.int(len(data)))
	dur = time.Duration(int(duration)) * time.Second / time.Duration(self.SampleRate)
	return
}

func (self *AudioDecoder) Close() {
	freeFFCtx(self.ff)
}

func NewAudioEncoderByName(name string) (enc *AudioEncoder, err error) {
	_enc := &AudioEncoder{}

	codec := C.avcodec_find_encoder_by_name(C.CString(name))
	if codec == nil || C.avcodec_get_type(codec.id) != C.AVMEDIA_TYPE_AUDIO {
		err = fmt.Errorf("ffmpeg: cannot find audio encoder name=%s", name)
		return
	}

	if _enc.ff, err = newFFCtxByCodec(codec); err != nil {
		return
	}
	enc = _enc
	return
}

func NewAudioDecoderByName(name string) (dec *AudioDecoder, err error) {
	_dec := &AudioDecoder{}

	codec := C.avcodec_find_decoder_by_name(C.CString(name))
	if codec == nil || C.avcodec_get_type(codec.id) != C.AVMEDIA_TYPE_AUDIO {
		err = fmt.Errorf("ffmpeg: cannot find audio decoder name=%s", name)
		return
	}

	if _dec.ff, err = newFFCtxByCodec(codec); err != nil {
		return
	}

	dec = _dec

	return

}

func NewAudioDecoder(codec av.AudioCodecData) (dec *AudioDecoder, err error) {
	_dec := &AudioDecoder{}
	var id uint32

	switch codec.Type() {
	case av.AAC:
		if aaccodec, ok := codec.(aac.CodecData); ok {
			_dec.Extradata = aaccodec.MPEG4AudioConfigBytes()
			id = C.AV_CODEC_ID_AAC
		} else {
			err = fmt.Errorf("ffmpeg: aac CodecData must be aacparser.CodecData")
			return
		}

	case av.SPEEX:
		id = C.AV_CODEC_ID_SPEEX

	case av.PCM_MULAW:
		id = C.AV_CODEC_ID_PCM_MULAW

	case av.PCM_ALAW:
		id = C.AV_CODEC_ID_PCM_ALAW

	default:
		if ffcodec, ok := codec.(audioCodecData); ok {
			_dec.Extradata = ffcodec.extradata
			id = ffcodec.codecId
		} else {
			err = fmt.Errorf("ffmpeg: invalid CodecData for ffmpeg to decode")
			return
		}
	}

	c := C.avcodec_find_decoder(id)
	if c == nil || C.avcodec_get_type(c.id) != C.AVMEDIA_TYPE_AUDIO {
		err = fmt.Errorf("ffmpeg: cannot find audio decoder id=%d", id)
		return
	}

	if _dec.ff, err = newFFCtxByCodec(c); err != nil {
		return
	}

	_dec.SampleFormat = codec.SampleFormat()
	_dec.SampleRate = codec.SampleRate()
	_dec.ChannelLayout = codec.ChannelLayout()

	dec = _dec
	return
}

type audioCodecData struct {
	codecId       uint32
	sampleFormat  av.SampleFormat
	channelLayout av.ChannelLayout
	sampleRate    int
	extradata     []byte
}

func (self audioCodecData) Type() av.CodecType {
	return av.MakeAudioCodecType(self.codecId)
}

func (self audioCodecData) SampleRate() int {
	return self.sampleRate
}

func (self audioCodecData) SampleFormat() av.SampleFormat {
	return self.sampleFormat
}

func (self audioCodecData) ChannelLayout() av.ChannelLayout {
	return self.channelLayout
}
