package bits

import (
	"io"
)

type Reader struct {
	R    io.Reader
	n    int
	bits uint64
}

func (self *Reader) ReadBits64(n int) (bits uint64, err error) {
	if self.n < n {
		var b [8]byte
		var got int
		want := (n - self.n + 7) / 8
		if got, err = self.R.Read(b[:want]); err != nil {
			return
		}
		if got < want {
			err = io.EOF
			return
		}
		for i := 0; i < got; i++ {
			self.bits <<= 8
			self.bits |= uint64(b[i])
		}
		self.n += got * 8
	}
	bits = self.bits >> uint(self.n-n)
	self.bits ^= bits << uint(self.n-n)
	self.n -= n
	return
}

func (self *Reader) ReadBits(n int) (bits uint, err error) {
	var bits64 uint64
	if bits64, err = self.ReadBits64(n); err != nil {
		return
	}
	bits = uint(bits64)
	return
}

func (self *Reader) Read(p []byte) (n int, err error) {
	for n < len(p) {
		want := 8
		if len(p)-n < want {
			want = len(p) - n
		}
		var bits uint64
		if bits, err = self.ReadBits64(want * 8); err != nil {
			break
		}
		for i := 0; i < want; i++ {
			p[n+i] = byte(bits >> uint((want-i-1)*8))
		}
		n += want
	}
	return
}

type Writer struct {
	W    io.Writer
	n    int
	bits uint64
}

func (self *Writer) WriteBits64(bits uint64, n int) (err error) {
	if self.n+n > 64 {
		move := uint(64 - self.n)
		mask := bits >> move
		self.bits = (self.bits << move) | mask
		self.n = 64
		if err = self.FlushBits(); err != nil {
			return
		}
		n -= int(move)
		bits ^= (mask << move)
	}
	self.bits = (self.bits << uint(n)) | bits
	self.n += n
	return
}

func (self *Writer) WriteBits(bits uint, n int) (err error) {
	return self.WriteBits64(uint64(bits), n)
}

func (self *Writer) Write(p []byte) (n int, err error) {
	for n < len(p) {
		if err = self.WriteBits64(uint64(p[n]), 8); err != nil {
			return
		}
		n++
	}
	return
}

func (self *Writer) FlushBits() (err error) {
	if self.n > 0 {
		var b [8]byte
		bits := self.bits
		if self.n%8 != 0 {
			bits <<= uint(8 - (self.n % 8))
		}
		want := (self.n + 7) / 8
		for i := 0; i < want; i++ {
			b[i] = byte(bits >> uint((want-i-1)*8))
		}
		if _, err = self.W.Write(b[:want]); err != nil {
			return
		}
		self.n = 0
	}
	return
}

type GolombBitReader struct {
	R    io.Reader
	buf  [1]byte
	left byte
}

func (self *GolombBitReader) ReadBit() (res uint, err error) {
	if self.left == 0 {
		if _, err = self.R.Read(self.buf[:]); err != nil {
			return
		}
		self.left = 8
	}
	self.left--
	res = uint(self.buf[0]>>self.left) & 1
	return
}

func (self *GolombBitReader) ReadBits(n int) (res uint, err error) {
	for i := 0; i < n; i++ {
		var bit uint
		if bit, err = self.ReadBit(); err != nil {
			return
		}
		res |= bit << uint(n-i-1)
	}
	return
}

func (self *GolombBitReader) ReadExponentialGolombCode() (res uint, err error) {
	i := 0
	for {
		var bit uint
		if bit, err = self.ReadBit(); err != nil {
			return
		}
		if !(bit == 0 && i < 32) {
			break
		}
		i++
	}
	if res, err = self.ReadBits(i); err != nil {
		return
	}
	res += (1 << uint(i)) - 1
	return
}

func (self *GolombBitReader) ReadSE() (res uint, err error) {
	if res, err = self.ReadExponentialGolombCode(); err != nil {
		return
	}
	if res&0x01 != 0 {
		res = (res + 1) / 2
	} else {
		res = -res / 2
	}
	return
}
