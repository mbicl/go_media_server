package main

import (
	"fmt"
	"log"
	"sync"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"

	"github.com/pion/rtp"
)

type serverHandler struct {
	server      *gortsplib.Server
	mutex       sync.Mutex
	publisher   *gortsplib.ServerSession
	media       *description.Media
	format      *format.H264
	rtpDec      *rtph264.Decoder
	stream      *gortsplib.ServerStream
	mpegtsMuxer *mpegtsMuxer
}

func (sh *serverHandler) OnConnOpen(_ *gortsplib.ServerHandlerOnConnOpenCtx) {
	log.Printf("Connection opened\n")
}

func (sh *serverHandler) OnConnClose(ctx *gortsplib.ServerHandlerOnConnCloseCtx) {
	log.Printf("Connection closed: %v\n", ctx.Error)
}

func (sh *serverHandler) OnSessionOpen(_ *gortsplib.ServerHandlerOnSessionOpenCtx) {
	log.Printf("Session opened\n")
}

func (sh *serverHandler) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {
	log.Printf("Session closed: %v\n", ctx.Error)

	sh.mutex.Lock()
	defer sh.mutex.Unlock()

	if sh.stream != nil && ctx.Session == sh.publisher {
		sh.stream.Close()
		sh.stream = nil
	}

	sh.publisher = nil
	sh.mpegtsMuxer.close()
}

func (sh *serverHandler) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	log.Printf("Describe request\n")

	sh.mutex.Lock()
	defer sh.mutex.Unlock()

	if sh.stream == nil {
		return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
	}

	return &base.Response{StatusCode: base.StatusOK}, sh.stream, nil
}

func (sh *serverHandler) OnAnnounce(ctx *gortsplib.ServerHandlerOnAnnounceCtx) (*base.Response, error) {
	log.Printf("Announce request\n")

	sh.mutex.Lock()
	defer sh.mutex.Unlock()

	if sh.publisher != nil && sh.stream != nil {
		sh.stream.Close()
		sh.publisher.Close()
		sh.mpegtsMuxer.close()
	}

	var forma *format.H264
	medi := ctx.Description.FindFormat(&forma)
	if medi == nil {
		return &base.Response{
			StatusCode: base.StatusBadRequest,
		}, fmt.Errorf("H264 media not found")
	}

	rtpDec, err := forma.CreateDecoder()
	if err != nil {
		panic(err)
	}

	mpegtsMuxer := &mpegtsMuxer{
		fileName: "mystream.ts",
		sps:      forma.SPS,
		pps:      forma.PPS,
	}
	err = mpegtsMuxer.initialize()
	if err != nil {
		return &base.Response{StatusCode: base.StatusBadRequest}, err
	}

	sh.publisher = ctx.Session
	sh.media = medi
	sh.format = forma
	sh.rtpDec = rtpDec
	sh.mpegtsMuxer = mpegtsMuxer
	sh.stream = &gortsplib.ServerStream{
		Server: sh.server,
		Desc:   ctx.Description,
	}
	err = sh.stream.Initialize()
	if err != nil {
		panic(err)
	}

	return &base.Response{StatusCode: base.StatusOK}, nil
}

func (sh *serverHandler) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	log.Printf("Setup request\n")

	if ctx.Session == sh.publisher {
		if ctx.Session.State() == gortsplib.ServerSessionStateInitial {
			return &base.Response{StatusCode: base.StatusNotImplemented}, nil, nil
		}
		return &base.Response{StatusCode: base.StatusOK}, nil, nil
	}

	if ctx.Session.State() == gortsplib.ServerSessionStatePreRecord {
		return &base.Response{StatusCode: base.StatusOK}, nil, nil
	}

	sh.mutex.Lock()
	defer sh.mutex.Unlock()

	if sh.stream == nil {
		return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
	}

	return &base.Response{StatusCode: base.StatusOK}, sh.stream, nil
}

func (sh *serverHandler) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	log.Printf("Play request\n")
	return &base.Response{StatusCode: base.StatusOK}, nil
}

func (sh *serverHandler) OnRecord(ctx *gortsplib.ServerHandlerOnRecordCtx) (*base.Response, error) {
	log.Printf("Record request\n")

	// ctx.Session.OnPacketRTP(sh.media, sh.format, func(pkt *rtp.Packet) {
	// 	pts, ok := ctx.Session.PacketPTS(sh.media, pkt)
	// 	if !ok {
	// 		return
	// 	}

	// 	au, err := sh.rtpDec.Decode(pkt)
	// 	if err != nil {
	// 		return
	// 	}

	// 	sh.mpegtsMuxer.writeH264(au, pts)
	// 	sh.stream.WritePacketRTP(sh.media, pkt)
	// })

	ctx.Session.OnPacketRTPAny(func(medi *description.Media, _ format.Format, pkt *rtp.Packet) {
		err := sh.stream.WritePacketRTP(medi, pkt)
		if err != nil {
			log.Printf("ERROR: %v\n", err)
		}

		pts, ok := ctx.Session.PacketPTS(medi, pkt)
		if !ok {
			return
		}

		au, err := sh.rtpDec.Decode(pkt)
		if err != nil {
			return
		}

		sh.mpegtsMuxer.writeH264(au, pts)
	})

	return &base.Response{StatusCode: base.StatusOK}, nil
}

func main() {
	h := &serverHandler{}
	h.server = &gortsplib.Server{
		Handler:           h,
		RTSPAddress:       ":8554",
		UDPRTPAddress:     ":8000",
		UDPRTCPAddress:    ":8001",
		MulticastIPRange:  "224.1.0.0/16",
		MulticastRTPPort:  8002,
		MulticastRTCPPort: 8003,
	}

	log.Printf("Server is ready on %v\n", h.server.RTSPAddress)
	panic(h.server.StartAndWait())
}
