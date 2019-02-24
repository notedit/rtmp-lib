package h264

import (
	"bytes"
	"fmt"

	"github.com/notedit/rtmp-lib/av"
	"github.com/notedit/rtmp-lib/bits"
	"github.com/notedit/rtmp-lib/pio"
)

const (
	NALU_SEI = 6
	NALU_PPS = 7
	NALU_SPS = 8
	NALU_AUD = 9
)

func IsDataNALU(b []byte) bool {
	typ := b[0] & 0x1f
	return typ >= 1 && typ <= 5
}

type SPSInfo struct {
	ProfileIdc uint
	LevelIdc   uint

	MbWidth  uint
	MbHeight uint

	CropLeft   uint
	CropRight  uint
	CropTop    uint
	CropBottom uint

	Width  uint
	Height uint
}

func ParseSPS(data []byte) (self SPSInfo, err error) {
	r := &bits.GolombBitReader{R: bytes.NewReader(data)}

	if _, err = r.ReadBits(8); err != nil {
		return
	}

	if self.ProfileIdc, err = r.ReadBits(8); err != nil {
		return
	}

	// constraint_set0_flag-constraint_set6_flag,reserved_zero_2bits
	if _, err = r.ReadBits(8); err != nil {
		return
	}

	// level_idc
	if self.LevelIdc, err = r.ReadBits(8); err != nil {
		return
	}

	// seq_parameter_set_id
	if _, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}

	if self.ProfileIdc == 100 || self.ProfileIdc == 110 ||
		self.ProfileIdc == 122 || self.ProfileIdc == 244 ||
		self.ProfileIdc == 44 || self.ProfileIdc == 83 ||
		self.ProfileIdc == 86 || self.ProfileIdc == 118 {

		var chroma_format_idc uint
		if chroma_format_idc, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}

		if chroma_format_idc == 3 {
			// residual_colour_transform_flag
			if _, err = r.ReadBit(); err != nil {
				return
			}
		}

		// bit_depth_luma_minus8
		if _, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		// bit_depth_chroma_minus8
		if _, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		// qpprime_y_zero_transform_bypass_flag
		if _, err = r.ReadBit(); err != nil {
			return
		}

		var seq_scaling_matrix_present_flag uint
		if seq_scaling_matrix_present_flag, err = r.ReadBit(); err != nil {
			return
		}

		if seq_scaling_matrix_present_flag != 0 {
			for i := 0; i < 8; i++ {
				var seq_scaling_list_present_flag uint
				if seq_scaling_list_present_flag, err = r.ReadBit(); err != nil {
					return
				}
				if seq_scaling_list_present_flag != 0 {
					var sizeOfScalingList uint
					if i < 6 {
						sizeOfScalingList = 16
					} else {
						sizeOfScalingList = 64
					}
					lastScale := uint(8)
					nextScale := uint(8)
					for j := uint(0); j < sizeOfScalingList; j++ {
						if nextScale != 0 {
							var delta_scale uint
							if delta_scale, err = r.ReadSE(); err != nil {
								return
							}
							nextScale = (lastScale + delta_scale + 256) % 256
						}
						if nextScale != 0 {
							lastScale = nextScale
						}
					}
				}
			}
		}
	}

	// log2_max_frame_num_minus4
	if _, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}

	var pic_order_cnt_type uint
	if pic_order_cnt_type, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}
	if pic_order_cnt_type == 0 {
		// log2_max_pic_order_cnt_lsb_minus4
		if _, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
	} else if pic_order_cnt_type == 1 {
		// delta_pic_order_always_zero_flag
		if _, err = r.ReadBit(); err != nil {
			return
		}
		// offset_for_non_ref_pic
		if _, err = r.ReadSE(); err != nil {
			return
		}
		// offset_for_top_to_bottom_field
		if _, err = r.ReadSE(); err != nil {
			return
		}
		var num_ref_frames_in_pic_order_cnt_cycle uint
		if num_ref_frames_in_pic_order_cnt_cycle, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		for i := uint(0); i < num_ref_frames_in_pic_order_cnt_cycle; i++ {
			if _, err = r.ReadSE(); err != nil {
				return
			}
		}
	}

	// max_num_ref_frames
	if _, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}

	// gaps_in_frame_num_value_allowed_flag
	if _, err = r.ReadBit(); err != nil {
		return
	}

	if self.MbWidth, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}
	self.MbWidth++

	if self.MbHeight, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}
	self.MbHeight++

	var frame_mbs_only_flag uint
	if frame_mbs_only_flag, err = r.ReadBit(); err != nil {
		return
	}
	if frame_mbs_only_flag == 0 {
		// mb_adaptive_frame_field_flag
		if _, err = r.ReadBit(); err != nil {
			return
		}
	}

	// direct_8x8_inference_flag
	if _, err = r.ReadBit(); err != nil {
		return
	}

	var frame_cropping_flag uint
	if frame_cropping_flag, err = r.ReadBit(); err != nil {
		return
	}
	if frame_cropping_flag != 0 {
		if self.CropLeft, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		if self.CropRight, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		if self.CropTop, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		if self.CropBottom, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
	}

	self.Width = (self.MbWidth * 16) - self.CropLeft*2 - self.CropRight*2
	self.Height = ((2 - frame_mbs_only_flag) * self.MbHeight * 16) - self.CropTop*2 - self.CropBottom*2

	return
}

type CodecData struct {
	Record     []byte
	RecordInfo AVCDecoderConfRecord
	SPSInfo    SPSInfo
}

func (self CodecData) Type() av.CodecType {
	return av.H264
}

func (self CodecData) AVCDecoderConfRecordBytes() []byte {
	return self.Record
}

func (self CodecData) SPS() []byte {
	return self.RecordInfo.SPS[0]
}

func (self CodecData) PPS() []byte {
	return self.RecordInfo.PPS[0]
}

func (self CodecData) Width() int {
	return int(self.SPSInfo.Width)
}

func (self CodecData) Height() int {
	return int(self.SPSInfo.Height)
}

func NewCodecDataFromAVCDecoderConfRecord(record []byte) (self CodecData, err error) {
	self.Record = record
	if _, err = (&self.RecordInfo).Unmarshal(record); err != nil {
		return
	}
	if len(self.RecordInfo.SPS) == 0 {
		err = fmt.Errorf("h264parser: no SPS found in AVCDecoderConfRecord")
		return
	}
	if len(self.RecordInfo.PPS) == 0 {
		err = fmt.Errorf("h264parser: no PPS found in AVCDecoderConfRecord")
		return
	}
	if self.SPSInfo, err = ParseSPS(self.RecordInfo.SPS[0]); err != nil {
		err = fmt.Errorf("h264parser: parse SPS failed(%s)", err)
		return
	}
	return
}

func NewCodecDataFromSPSAndPPS(sps, pps []byte) (self CodecData, err error) {
	recordinfo := AVCDecoderConfRecord{}
	recordinfo.AVCProfileIndication = sps[1]
	recordinfo.ProfileCompatibility = sps[2]
	recordinfo.AVCLevelIndication = sps[3]
	recordinfo.SPS = [][]byte{sps}
	recordinfo.PPS = [][]byte{pps}
	recordinfo.LengthSizeMinusOne = 3

	buf := make([]byte, recordinfo.Len())
	recordinfo.Marshal(buf)

	self.RecordInfo = recordinfo
	self.Record = buf

	if self.SPSInfo, err = ParseSPS(sps); err != nil {
		return
	}
	return
}

type AVCDecoderConfRecord struct {
	AVCProfileIndication uint8
	ProfileCompatibility uint8
	AVCLevelIndication   uint8
	LengthSizeMinusOne   uint8
	SPS                  [][]byte
	PPS                  [][]byte
}

var ErrDecconfInvalid = fmt.Errorf("h264parser: AVCDecoderConfRecord invalid")

func (self *AVCDecoderConfRecord) Unmarshal(b []byte) (n int, err error) {
	if len(b) < 7 {
		err = ErrDecconfInvalid
		return
	}

	self.AVCProfileIndication = b[1]
	self.ProfileCompatibility = b[2]
	self.AVCLevelIndication = b[3]
	self.LengthSizeMinusOne = b[4] & 0x03
	spscount := int(b[5] & 0x1f)
	n += 6

	for i := 0; i < spscount; i++ {
		if len(b) < n+2 {
			err = ErrDecconfInvalid
			return
		}
		spslen := int(pio.U16BE(b[n:]))
		n += 2

		if len(b) < n+spslen {
			err = ErrDecconfInvalid
			return
		}
		self.SPS = append(self.SPS, b[n:n+spslen])
		n += spslen
	}

	if len(b) < n+1 {
		err = ErrDecconfInvalid
		return
	}
	ppscount := int(b[n])
	n++

	for i := 0; i < ppscount; i++ {
		if len(b) < n+2 {
			err = ErrDecconfInvalid
			return
		}
		ppslen := int(pio.U16BE(b[n:]))
		n += 2

		if len(b) < n+ppslen {
			err = ErrDecconfInvalid
			return
		}
		self.PPS = append(self.PPS, b[n:n+ppslen])
		n += ppslen
	}

	return
}

func (self AVCDecoderConfRecord) Len() (n int) {
	n = 7
	for _, sps := range self.SPS {
		n += 2 + len(sps)
	}
	for _, pps := range self.PPS {
		n += 2 + len(pps)
	}
	return
}

func (self AVCDecoderConfRecord) Marshal(b []byte) (n int) {
	b[0] = 1
	b[1] = self.AVCProfileIndication
	b[2] = self.ProfileCompatibility
	b[3] = self.AVCLevelIndication
	b[4] = self.LengthSizeMinusOne | 0xfc
	b[5] = uint8(len(self.SPS)) | 0xe0
	n += 6

	for _, sps := range self.SPS {
		pio.PutU16BE(b[n:], uint16(len(sps)))
		n += 2
		copy(b[n:], sps)
		n += len(sps)
	}

	b[n] = uint8(len(self.PPS))
	n++

	for _, pps := range self.PPS {
		pio.PutU16BE(b[n:], uint16(len(pps)))
		n += 2
		copy(b[n:], pps)
		n += len(pps)
	}

	return
}
