package main

import (
	"bufio"
	"os"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mpegts"
)

type mpegtsMuxer struct {
	fileName string
	sps      []byte
	pps      []byte

	f            *os.File
	b            *bufio.Writer
	w            *mpegts.Writer
	track        *mpegts.Track
	dtsExtractor *h264.DTSExtractor
}

func (e *mpegtsMuxer) initialize() error {
	var err error

	e.f, err = os.Create(e.fileName)
	if err != nil {
		return err
	}
	e.b = bufio.NewWriter(e.f)

	e.track = &mpegts.Track{
		Codec: &mpegts.CodecH264{},
	}

	e.w = &mpegts.Writer{
		W:      e.b,
		Tracks: []*mpegts.Track{e.track},
	}
	err = e.w.Initialize()
	if err != nil {
		return err
	}

	return nil
}

func (e *mpegtsMuxer) close() {
	e.b.Flush()
	e.f.Close()
}

func (e *mpegtsMuxer) writeH264(au [][]byte, pts int64) error {
	var filteredAU [][]byte

	nonIDRPresent := false
	idrPresent := false

	for _, nalu := range au {
		typ := h264.NALUType(nalu[0] & 0x1F)
		switch typ {
		case h264.NALUTypeSPS:
			e.sps = nalu
			continue
		case h264.NALUTypePPS:
			e.pps = nalu
			continue
		case h264.NALUTypeAccessUnitDelimiter:
			continue
		case h264.NALUTypeIDR:
			idrPresent = true
		case h264.NALUTypeNonIDR:
			nonIDRPresent = true
		}

		filteredAU = append(filteredAU, nalu)
	}

	au = filteredAU

	if au == nil || (!idrPresent && !nonIDRPresent) {
		return nil
	}

	if idrPresent {
		au = append([][]byte{e.sps, e.pps}, au...)
	}

	if e.dtsExtractor == nil {
		if !idrPresent {
			return nil
		}
		e.dtsExtractor = &h264.DTSExtractor{}
		e.dtsExtractor.Initialize()
	}

	dts, err := e.dtsExtractor.Extract(au, pts)
	if err != nil {
		return err
	}

	return e.w.WriteH264(e.track, pts, dts, au)
}
