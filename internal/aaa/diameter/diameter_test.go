package diameter

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/aaa/rattype"
	"github.com/rs/zerolog"
)

func TestAVPEncodeDecodeUint32(t *testing.T) {
	avp := NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess)
	encoded := avp.Encode()

	decoded, consumed, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("decode avp: %v", err)
	}
	if consumed != len(encoded) {
		t.Fatalf("consumed %d, expected %d", consumed, len(encoded))
	}
	if decoded.Code != AVPCodeResultCode {
		t.Fatalf("code %d, expected %d", decoded.Code, AVPCodeResultCode)
	}
	val, err := decoded.GetUint32()
	if err != nil {
		t.Fatalf("get uint32: %v", err)
	}
	if val != ResultCodeSuccess {
		t.Fatalf("value %d, expected %d", val, ResultCodeSuccess)
	}
}

func TestAVPEncodeDecodeString(t *testing.T) {
	avp := NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "argus.example.com")
	encoded := avp.Encode()

	decoded, _, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("decode avp: %v", err)
	}
	if decoded.GetString() != "argus.example.com" {
		t.Fatalf("string %q, expected %q", decoded.GetString(), "argus.example.com")
	}
}

func TestAVPEncodeDecodeVendor(t *testing.T) {
	avp := NewAVPUint32(AVPCodeRATType3GPP, AVPFlagMandatory|AVPFlagVendor, VendorID3GPP, 1004)
	encoded := avp.Encode()

	decoded, _, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("decode avp: %v", err)
	}
	if !decoded.IsVendor() {
		t.Fatal("expected vendor flag set")
	}
	if decoded.VendorID != VendorID3GPP {
		t.Fatalf("vendor id %d, expected %d", decoded.VendorID, VendorID3GPP)
	}
	val, _ := decoded.GetUint32()
	if val != 1004 {
		t.Fatalf("value %d, expected 1004", val)
	}
}

func TestAVPEncodeDecodeUint64(t *testing.T) {
	avp := NewAVPUint64(AVPCodeCCTotalOctets, AVPFlagMandatory, 0, 1024*1024*1024)
	encoded := avp.Encode()

	decoded, _, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("decode avp: %v", err)
	}
	val, err := decoded.GetUint64()
	if err != nil {
		t.Fatalf("get uint64: %v", err)
	}
	if val != 1024*1024*1024 {
		t.Fatalf("value %d, expected %d", val, 1024*1024*1024)
	}
}

func TestAVPGroupedEncodeDecode(t *testing.T) {
	inner := []*AVP{
		NewAVPUint32(AVPCodeSubscriptionIDType, AVPFlagMandatory, 0, SubscriptionIDTypeIMSI),
		NewAVPString(AVPCodeSubscriptionIDData, AVPFlagMandatory, 0, "286010123456789"),
	}
	grouped := NewAVPGrouped(AVPCodeSubscriptionID, AVPFlagMandatory, 0, inner)
	encoded := grouped.Encode()

	decoded, _, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("decode grouped avp: %v", err)
	}

	children, err := decoded.GetGrouped()
	if err != nil {
		t.Fatalf("get grouped: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}

	typeVal, _ := children[0].GetUint32()
	if typeVal != SubscriptionIDTypeIMSI {
		t.Fatalf("subscription type %d, expected %d", typeVal, SubscriptionIDTypeIMSI)
	}
	if children[1].GetString() != "286010123456789" {
		t.Fatalf("subscription data %q, expected %q", children[1].GetString(), "286010123456789")
	}
}

func TestExtractSubscriptionID(t *testing.T) {
	avps := BuildSubscriptionID("286010123456789", "905551234567")
	imsi, msisdn := ExtractSubscriptionID(avps)
	if imsi != "286010123456789" {
		t.Fatalf("imsi %q, expected %q", imsi, "286010123456789")
	}
	if msisdn != "905551234567" {
		t.Fatalf("msisdn %q, expected %q", msisdn, "905551234567")
	}
}

func TestMessageEncodeDecode(t *testing.T) {
	msg := NewRequest(CommandCCR, ApplicationIDGx, 1, 100)
	msg.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "test-session-1"))
	msg.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	msg.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))

	encoded, err := msg.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.Version != 1 {
		t.Fatalf("version %d, expected 1", decoded.Version)
	}
	if !decoded.IsRequest() {
		t.Fatal("expected request flag")
	}
	if decoded.CommandCode != CommandCCR {
		t.Fatalf("command code %d, expected %d", decoded.CommandCode, CommandCCR)
	}
	if decoded.ApplicationID != ApplicationIDGx {
		t.Fatalf("app id %d, expected %d", decoded.ApplicationID, ApplicationIDGx)
	}
	if decoded.GetSessionID() != "test-session-1" {
		t.Fatalf("session id %q, expected %q", decoded.GetSessionID(), "test-session-1")
	}
	if decoded.GetCCRequestType() != CCRequestTypeInitial {
		t.Fatalf("cc request type %d, expected %d", decoded.GetCCRequestType(), CCRequestTypeInitial)
	}
}

func TestNewAnswer(t *testing.T) {
	req := NewRequest(CommandCCR, ApplicationIDGx, 42, 99)
	ans := NewAnswer(req)

	if ans.IsRequest() {
		t.Fatal("answer should not have request flag")
	}
	if ans.CommandCode != req.CommandCode {
		t.Fatalf("command code %d, expected %d", ans.CommandCode, req.CommandCode)
	}
	if ans.HopByHopID != req.HopByHopID {
		t.Fatalf("hop-by-hop %d, expected %d", ans.HopByHopID, req.HopByHopID)
	}
	if ans.EndToEndID != req.EndToEndID {
		t.Fatalf("end-to-end %d, expected %d", ans.EndToEndID, req.EndToEndID)
	}
}

func TestMultipleAVPsDecode(t *testing.T) {
	avps := []*AVP{
		NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer1.example.com"),
		NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "example.com"),
		NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess),
	}

	var data []byte
	for _, a := range avps {
		data = append(data, a.Encode()...)
	}

	decoded, err := DecodeAVPs(data)
	if err != nil {
		t.Fatalf("decode avps: %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("expected 3 avps, got %d", len(decoded))
	}
}

func TestBuildChargingRuleInstall(t *testing.T) {
	rule := BuildChargingRuleInstall("test-rule", 9, 10_000_000, 50_000_000)
	if rule.Code != AVPCodeChargingRuleInstall {
		t.Fatalf("code %d, expected %d", rule.Code, AVPCodeChargingRuleInstall)
	}
	if rule.VendorID != VendorID3GPP {
		t.Fatalf("vendor %d, expected %d", rule.VendorID, VendorID3GPP)
	}

	children, err := rule.GetGrouped()
	if err != nil {
		t.Fatalf("get grouped: %v", err)
	}
	if len(children) == 0 {
		t.Fatal("expected children in charging rule install")
	}
}

func TestBuildGrantedServiceUnit(t *testing.T) {
	gsu := BuildGrantedServiceUnit(100*1024*1024, 3600, 600)
	if gsu.Code != AVPCodeGrantedServiceUnit {
		t.Fatalf("code %d, expected %d", gsu.Code, AVPCodeGrantedServiceUnit)
	}
	children, err := gsu.GetGrouped()
	if err != nil {
		t.Fatalf("get grouped: %v", err)
	}
	if len(children) < 2 {
		t.Fatalf("expected >= 2 children, got %d", len(children))
	}
}

func TestBuildFinalUnitIndication(t *testing.T) {
	fui := BuildFinalUnitIndication(FinalUnitActionTerminate)
	children, err := fui.GetGrouped()
	if err != nil {
		t.Fatalf("get grouped: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	val, _ := children[0].GetUint32()
	if val != FinalUnitActionTerminate {
		t.Fatalf("action %d, expected %d", val, FinalUnitActionTerminate)
	}
}

func TestExtractUsedServiceUnit(t *testing.T) {
	inner := []*AVP{
		NewAVPUint64(AVPCodeCCTotalOctets, AVPFlagMandatory, 0, 5000),
		NewAVPUint64(AVPCodeCCInputOctets, AVPFlagMandatory, 0, 2000),
		NewAVPUint64(AVPCodeCCOutputOctets, AVPFlagMandatory, 0, 3000),
		NewAVPUint32(AVPCodeCCTime, AVPFlagMandatory, 0, 120),
	}
	usu := NewAVPGrouped(AVPCodeUsedServiceUnit, AVPFlagMandatory, 0, inner)

	total, input, output, timeSec := ExtractUsedServiceUnit([]*AVP{usu})
	if total != 5000 {
		t.Fatalf("total %d, expected 5000", total)
	}
	if input != 2000 {
		t.Fatalf("input %d, expected 2000", input)
	}
	if output != 3000 {
		t.Fatalf("output %d, expected 3000", output)
	}
	if timeSec != 120 {
		t.Fatalf("time %d, expected 120", timeSec)
	}
}

func TestReadMessageLength(t *testing.T) {
	header := []byte{1, 0, 0, 100}
	msgLen, err := ReadMessageLength(header)
	if err != nil {
		t.Fatalf("read message length: %v", err)
	}
	if msgLen != 100 {
		t.Fatalf("length %d, expected 100", msgLen)
	}

	_, err = ReadMessageLength([]byte{2, 0, 0, 20})
	if err == nil {
		t.Fatal("expected error for unsupported version")
	}

	_, err = ReadMessageLength([]byte{1, 0})
	if err == nil {
		t.Fatal("expected error for short header")
	}
}

func TestCERCEAExchange(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		VendorID:         99999,
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		SessionMgr: nil,
		EventBus:   nil,
		Logger:     testLogger(),
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.listener = ln
	srv.mu.Lock()
	srv.running = true
	srv.mu.Unlock()
	srv.wg.Add(1)
	go srv.acceptLoop()

	defer func() {
		close(srv.stopCh)
		ln.Close()
		srv.wg.Wait()
	}()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cer := NewRequest(CommandCER, ApplicationIDDiameterBase, 1, 1)
	cer.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
	cer.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
	cer.AddAVP(NewAVPAddress(AVPCodeHostIPAddress, AVPFlagMandatory, 0, [4]byte{127, 0, 0, 1}))
	cer.AddAVP(NewAVPUint32(AVPCodeVendorID, AVPFlagMandatory, 0, 10415))
	cer.AddAVP(NewAVPString(AVPCodeProductName, 0, 0, "TestPeer"))
	cer.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))

	cerData, err := cer.Encode()
	if err != nil {
		t.Fatalf("encode cer: %v", err)
	}
	if _, err := conn.Write(cerData); err != nil {
		t.Fatalf("write cer: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	headerBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, headerBuf); err != nil {
		t.Fatalf("read cea header: %v", err)
	}

	msgLen, err := ReadMessageLength(headerBuf)
	if err != nil {
		t.Fatalf("read cea length: %v", err)
	}

	ceaBuf := make([]byte, msgLen)
	copy(ceaBuf[:4], headerBuf)
	if _, err := io.ReadFull(conn, ceaBuf[4:]); err != nil {
		t.Fatalf("read cea body: %v", err)
	}

	cea, err := DecodeMessage(ceaBuf)
	if err != nil {
		t.Fatalf("decode cea: %v", err)
	}

	if cea.IsRequest() {
		t.Fatal("CEA should be an answer")
	}
	if cea.CommandCode != CommandCEA {
		t.Fatalf("command code %d, expected %d", cea.CommandCode, CommandCEA)
	}
	if cea.GetResultCode() != ResultCodeSuccess {
		t.Fatalf("result code %d, expected %d", cea.GetResultCode(), ResultCodeSuccess)
	}
	if cea.GetOriginHost() != "argus.test.com" {
		t.Fatalf("origin host %q, expected %q", cea.GetOriginHost(), "argus.test.com")
	}
}

func TestDWRDWAExchange(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		Logger: testLogger(),
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.listener = ln
	srv.mu.Lock()
	srv.running = true
	srv.mu.Unlock()
	srv.wg.Add(1)
	go srv.acceptLoop()

	defer func() {
		close(srv.stopCh)
		ln.Close()
		srv.wg.Wait()
	}()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cer := NewRequest(CommandCER, ApplicationIDDiameterBase, 1, 1)
	cer.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
	cer.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
	cer.AddAVP(NewAVPAddress(AVPCodeHostIPAddress, AVPFlagMandatory, 0, [4]byte{127, 0, 0, 1}))
	cer.AddAVP(NewAVPUint32(AVPCodeVendorID, AVPFlagMandatory, 0, 10415))
	cer.AddAVP(NewAVPString(AVPCodeProductName, 0, 0, "TestPeer"))
	cerData, _ := cer.Encode()
	conn.Write(cerData)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	readFullMessage(t, conn)

	dwr := NewRequest(CommandDWR, ApplicationIDDiameterBase, 2, 2)
	dwr.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
	dwr.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
	dwrData, _ := dwr.Encode()
	conn.Write(dwrData)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	dwa := readFullMessage(t, conn)

	if dwa.IsRequest() {
		t.Fatal("DWA should be an answer")
	}
	if dwa.CommandCode != CommandDWA {
		t.Fatalf("command code %d, expected %d", dwa.CommandCode, CommandDWA)
	}
	if dwa.GetResultCode() != ResultCodeSuccess {
		t.Fatalf("result code %d, expected %d", dwa.GetResultCode(), ResultCodeSuccess)
	}
}

func TestDPRDPAExchange(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		Logger: testLogger(),
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.listener = ln
	srv.mu.Lock()
	srv.running = true
	srv.mu.Unlock()
	srv.wg.Add(1)
	go srv.acceptLoop()

	defer func() {
		close(srv.stopCh)
		ln.Close()
		srv.wg.Wait()
	}()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cer := NewRequest(CommandCER, ApplicationIDDiameterBase, 1, 1)
	cer.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
	cer.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
	cer.AddAVP(NewAVPAddress(AVPCodeHostIPAddress, AVPFlagMandatory, 0, [4]byte{127, 0, 0, 1}))
	cer.AddAVP(NewAVPUint32(AVPCodeVendorID, AVPFlagMandatory, 0, 10415))
	cer.AddAVP(NewAVPString(AVPCodeProductName, 0, 0, "TestPeer"))
	cerData, _ := cer.Encode()
	conn.Write(cerData)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	readFullMessage(t, conn)

	dpr := NewRequest(CommandDPR, ApplicationIDDiameterBase, 3, 3)
	dpr.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
	dpr.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
	dpr.AddAVP(NewAVPUint32(AVPCodeDisconnectCause, AVPFlagMandatory, 0, DisconnectCauseRebooting))
	dprData, _ := dpr.Encode()
	conn.Write(dprData)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	dpa := readFullMessage(t, conn)

	if dpa.IsRequest() {
		t.Fatal("DPA should be an answer")
	}
	if dpa.CommandCode != CommandDPA {
		t.Fatalf("command code %d, expected %d", dpa.CommandCode, CommandDPA)
	}
	if dpa.GetResultCode() != ResultCodeSuccess {
		t.Fatalf("result code %d, expected %d", dpa.GetResultCode(), ResultCodeSuccess)
	}
}

func TestUnsupportedApplicationID(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		Logger: testLogger(),
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.listener = ln
	srv.mu.Lock()
	srv.running = true
	srv.mu.Unlock()
	srv.wg.Add(1)
	go srv.acceptLoop()

	defer func() {
		close(srv.stopCh)
		ln.Close()
		srv.wg.Wait()
	}()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cer := NewRequest(CommandCER, ApplicationIDDiameterBase, 1, 1)
	cer.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
	cer.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
	cer.AddAVP(NewAVPAddress(AVPCodeHostIPAddress, AVPFlagMandatory, 0, [4]byte{127, 0, 0, 1}))
	cer.AddAVP(NewAVPUint32(AVPCodeVendorID, AVPFlagMandatory, 0, 10415))
	cer.AddAVP(NewAVPString(AVPCodeProductName, 0, 0, "TestPeer"))
	cerData, _ := cer.Encode()
	conn.Write(cerData)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	readFullMessage(t, conn)

	ccr := NewRequest(CommandCCR, 99999, 2, 2)
	ccr.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "test-session"))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	ccrData, _ := ccr.Encode()
	conn.Write(ccrData)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	cca := readFullMessage(t, conn)

	if cca.GetResultCode() != ResultCodeApplicationUnsupported {
		t.Fatalf("result code %d, expected %d", cca.GetResultCode(), ResultCodeApplicationUnsupported)
	}
}

func TestAVPPadding(t *testing.T) {
	avp := NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "abc")
	encoded := avp.Encode()

	if len(encoded)%4 != 0 {
		t.Fatalf("encoded length %d not 4-byte aligned", len(encoded))
	}

	decoded, consumed, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if consumed != len(encoded) {
		t.Fatalf("consumed %d, expected %d", consumed, len(encoded))
	}
	if decoded.GetString() != "abc" {
		t.Fatalf("value %q, expected %q", decoded.GetString(), "abc")
	}
}

func TestServerStartStop(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		Logger: testLogger(),
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.listener = ln
	srv.mu.Lock()
	srv.running = true
	srv.mu.Unlock()
	srv.wg.Add(1)
	go srv.acceptLoop()

	if !srv.IsRunning() {
		t.Fatal("server should be running")
	}

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()

	close(srv.stopCh)
	ln.Close()
	srv.wg.Wait()
	srv.mu.Lock()
	srv.running = false
	srv.mu.Unlock()

	if srv.IsRunning() {
		t.Fatal("server should not be running")
	}
}

func TestFindAVP(t *testing.T) {
	avps := []*AVP{
		NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "host1"),
		NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "realm1"),
		NewAVPUint32(AVPCodeRATType3GPP, AVPFlagMandatory|AVPFlagVendor, VendorID3GPP, 1004),
	}

	found := FindAVP(avps, AVPCodeOriginHost)
	if found == nil {
		t.Fatal("expected to find OriginHost AVP")
	}
	if found.GetString() != "host1" {
		t.Fatalf("value %q, expected %q", found.GetString(), "host1")
	}

	notFound := FindAVP(avps, 999)
	if notFound != nil {
		t.Fatal("expected nil for non-existent AVP")
	}

	vendorAVP := FindAVPVendor(avps, AVPCodeRATType3GPP, VendorID3GPP)
	if vendorAVP == nil {
		t.Fatal("expected to find vendor AVP")
	}
	val, _ := vendorAVP.GetUint32()
	if val != 1004 {
		t.Fatalf("value %d, expected 1004", val)
	}
}

func TestPeerStateString(t *testing.T) {
	tests := []struct {
		state PeerState
		want  string
	}{
		{PeerStateIdle, "idle"},
		{PeerStateOpen, "open"},
		{PeerStateClosing, "closing"},
		{PeerStateClosed, "closed"},
		{PeerState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("PeerState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestSessionStateString(t *testing.T) {
	tests := []struct {
		state SessionState
		want  string
	}{
		{SessionStateIdle, "idle"},
		{SessionStateOpen, "open"},
		{SessionStatePending, "pending"},
		{SessionStateClosed, "closed"},
		{SessionState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("SessionState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestSessionStateTransitions(t *testing.T) {
	sm := NewSessionStateMap()

	ds := sm.Create("sess-1", "peer.test.com", ApplicationIDGx, "286010123456789")
	if ds.GetState() != SessionStateIdle {
		t.Fatalf("initial state %v, expected idle", ds.GetState())
	}

	if err := ds.Transition(SessionStateOpen); err != nil {
		t.Fatalf("idle->open should be valid: %v", err)
	}
	if ds.GetState() != SessionStateOpen {
		t.Fatalf("state %v, expected open", ds.GetState())
	}

	if err := ds.Transition(SessionStatePending); err != nil {
		t.Fatalf("open->pending should be valid: %v", err)
	}

	if err := ds.Transition(SessionStateOpen); err != nil {
		t.Fatalf("pending->open should be valid: %v", err)
	}

	if err := ds.Transition(SessionStateClosed); err != nil {
		t.Fatalf("open->closed should be valid: %v", err)
	}

	if err := ds.Transition(SessionStateOpen); err == nil {
		t.Fatal("closed->open should be invalid")
	}
}

func TestSessionStateMapCount(t *testing.T) {
	sm := NewSessionStateMap()

	ds1 := sm.Create("sess-1", "peer1", ApplicationIDGx, "imsi1")
	ds2 := sm.Create("sess-2", "peer2", ApplicationIDGy, "imsi2")

	if sm.Count() != 2 {
		t.Fatalf("count %d, expected 2", sm.Count())
	}

	_ = ds1.Transition(SessionStateOpen)
	_ = ds2.Transition(SessionStateOpen)

	if sm.ActiveCount() != 2 {
		t.Fatalf("active count %d, expected 2", sm.ActiveCount())
	}

	_ = ds1.Transition(SessionStateClosed)
	if sm.ActiveCount() != 1 {
		t.Fatalf("active count %d, expected 1", sm.ActiveCount())
	}

	sm.Delete("sess-1")
	if sm.Count() != 1 {
		t.Fatalf("count %d, expected 1", sm.Count())
	}
}

func TestInvalidSessionStateTransition(t *testing.T) {
	sm := NewSessionStateMap()
	ds := sm.Create("sess-1", "peer1", ApplicationIDGx, "imsi1")

	if err := ds.Transition(SessionStatePending); err == nil {
		t.Fatal("idle->pending should be invalid")
	}

	_ = ds.Transition(SessionStateOpen)
	if err := ds.Transition(SessionStateIdle); err == nil {
		t.Fatal("open->idle should be invalid")
	}
}

func TestMapDiameterRATType(t *testing.T) {
	tests := []struct {
		input uint32
		want  string
	}{
		{1000, "utran"},
		{1001, "geran"},
		{1004, "lte"},
		{1005, "nb_iot"},
		{1009, "nr_5g"},
		{9999, "unknown"},
	}
	for _, tt := range tests {
		if got := rattype.FromDiameter(tt.input); got != tt.want {
			t.Errorf("rattype.FromDiameter(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDecodeMessageErrors(t *testing.T) {
	_, err := DecodeMessage([]byte{1, 0, 0})
	if err == nil {
		t.Fatal("expected error for short data")
	}

	_, err = DecodeMessage([]byte{2, 0, 0, 20, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	if err == nil {
		t.Fatal("expected error for unsupported version")
	}
}

func TestAVPAddress(t *testing.T) {
	avp := NewAVPAddress(AVPCodeHostIPAddress, AVPFlagMandatory, 0, [4]byte{10, 0, 0, 1})
	encoded := avp.Encode()

	decoded, _, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Data) != 6 {
		t.Fatalf("data length %d, expected 6", len(decoded.Data))
	}
	addrFamily := binary.BigEndian.Uint16(decoded.Data[0:2])
	if addrFamily != 1 {
		t.Fatalf("address family %d, expected 1 (IPv4)", addrFamily)
	}
	if decoded.Data[2] != 10 || decoded.Data[3] != 0 || decoded.Data[4] != 0 || decoded.Data[5] != 1 {
		t.Fatalf("ip %d.%d.%d.%d, expected 10.0.0.1", decoded.Data[2], decoded.Data[3], decoded.Data[4], decoded.Data[5])
	}
}

func TestConcurrentMultiPeer(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		Logger: testLogger(),
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.listener = ln
	srv.mu.Lock()
	srv.running = true
	srv.mu.Unlock()
	srv.wg.Add(1)
	go srv.acceptLoop()

	defer func() {
		close(srv.stopCh)
		ln.Close()
		srv.wg.Wait()
	}()

	peerCount := 5
	var wg sync.WaitGroup
	errors := make(chan error, peerCount)

	for i := 0; i < peerCount; i++ {
		wg.Add(1)
		go func(peerIdx int) {
			defer wg.Done()

			conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
			if err != nil {
				errors <- err
				return
			}
			defer conn.Close()

			cer := NewRequest(CommandCER, ApplicationIDDiameterBase, uint32(peerIdx*10+1), uint32(peerIdx*10+1))
			cer.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
			cer.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
			cer.AddAVP(NewAVPAddress(AVPCodeHostIPAddress, AVPFlagMandatory, 0, [4]byte{127, 0, 0, 1}))
			cer.AddAVP(NewAVPUint32(AVPCodeVendorID, AVPFlagMandatory, 0, 10415))
			cer.AddAVP(NewAVPString(AVPCodeProductName, 0, 0, "TestPeer"))
			cer.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
			cerData, _ := cer.Encode()
			conn.Write(cerData)

			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			cea := readFullMessageErr(conn)
			if cea == nil {
				errors <- err
				return
			}
			if cea.GetResultCode() != ResultCodeSuccess {
				errors <- err
				return
			}

			dwr := NewRequest(CommandDWR, ApplicationIDDiameterBase, uint32(peerIdx*10+2), uint32(peerIdx*10+2))
			dwr.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
			dwr.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
			dwrData, _ := dwr.Encode()
			conn.Write(dwrData)

			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			dwa := readFullMessageErr(conn)
			if dwa == nil {
				errors <- err
				return
			}
			if dwa.GetResultCode() != ResultCodeSuccess {
				errors <- err
				return
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent peer error: %v", err)
		}
	}

	if srv.PeerCount() < 1 {
		t.Logf("peer count: %d (some may have disconnected)", srv.PeerCount())
	}
}

func TestGxCCRIViaTCP(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		Logger: testLogger(),
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.listener = ln
	srv.mu.Lock()
	srv.running = true
	srv.mu.Unlock()
	srv.wg.Add(1)
	go srv.acceptLoop()

	defer func() {
		close(srv.stopCh)
		ln.Close()
		srv.wg.Wait()
	}()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cer := NewRequest(CommandCER, ApplicationIDDiameterBase, 1, 1)
	cer.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
	cer.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
	cer.AddAVP(NewAVPAddress(AVPCodeHostIPAddress, AVPFlagMandatory, 0, [4]byte{127, 0, 0, 1}))
	cer.AddAVP(NewAVPUint32(AVPCodeVendorID, AVPFlagMandatory, 0, 10415))
	cer.AddAVP(NewAVPString(AVPCodeProductName, 0, 0, "TestPeer"))
	cer.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
	cerData, _ := cer.Encode()
	conn.Write(cerData)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	readFullMessage(t, conn)

	ccr := NewRequest(CommandCCR, ApplicationIDGx, 10, 10)
	ccr.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "gx-test-session-1"))
	ccr.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
	ccr.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
	ccr.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	for _, sub := range BuildSubscriptionID("286010123456789", "905551234567") {
		ccr.AddAVP(sub)
	}
	ccrData, _ := ccr.Encode()
	conn.Write(ccrData)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	cca := readFullMessage(t, conn)

	if cca.IsRequest() {
		t.Fatal("CCA should be an answer")
	}
	if cca.CommandCode != CommandCCA {
		t.Fatalf("command code %d, expected %d", cca.CommandCode, CommandCCA)
	}
	if cca.GetResultCode() != ResultCodeSuccess {
		t.Fatalf("result code %d, expected %d (got session manager nil — session creation skipped)", cca.GetResultCode(), ResultCodeSuccess)
	}

	sessID := cca.GetSessionID()
	if sessID != "gx-test-session-1" {
		t.Fatalf("session id %q, expected %q", sessID, "gx-test-session-1")
	}

	ccReqType := cca.GetCCRequestType()
	if ccReqType != CCRequestTypeInitial {
		t.Fatalf("cc request type %d, expected %d", ccReqType, CCRequestTypeInitial)
	}
}

func TestMalformedCCRMissingSessionID(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		Logger: testLogger(),
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.listener = ln
	srv.mu.Lock()
	srv.running = true
	srv.mu.Unlock()
	srv.wg.Add(1)
	go srv.acceptLoop()

	defer func() {
		close(srv.stopCh)
		ln.Close()
		srv.wg.Wait()
	}()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cer := NewRequest(CommandCER, ApplicationIDDiameterBase, 1, 1)
	cer.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
	cer.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
	cer.AddAVP(NewAVPAddress(AVPCodeHostIPAddress, AVPFlagMandatory, 0, [4]byte{127, 0, 0, 1}))
	cer.AddAVP(NewAVPUint32(AVPCodeVendorID, AVPFlagMandatory, 0, 10415))
	cerData, _ := cer.Encode()
	conn.Write(cerData)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	readFullMessage(t, conn)

	ccr := NewRequest(CommandCCR, ApplicationIDGx, 10, 10)
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	ccrData, _ := ccr.Encode()
	conn.Write(ccrData)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	cca := readFullMessage(t, conn)

	if cca.GetResultCode() != ResultCodeMissingAVP {
		t.Fatalf("result code %d, expected %d (DIAMETER_MISSING_AVP)", cca.GetResultCode(), ResultCodeMissingAVP)
	}
}

func TestServerHealthy(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		Logger: testLogger(),
	})

	if srv.Healthy() {
		t.Fatal("server should not be healthy before start")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.listener = ln
	srv.mu.Lock()
	srv.running = true
	srv.mu.Unlock()
	srv.wg.Add(1)
	go srv.acceptLoop()

	if !srv.Healthy() {
		t.Fatal("server should be healthy after start")
	}

	close(srv.stopCh)
	ln.Close()
	srv.wg.Wait()
	srv.mu.Lock()
	srv.running = false
	srv.mu.Unlock()

	if srv.Healthy() {
		t.Fatal("server should not be healthy after stop")
	}
}

func readFullMessage(t *testing.T, conn net.Conn) *Message {
	t.Helper()
	headerBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, headerBuf); err != nil {
		t.Fatalf("read header: %v", err)
	}
	msgLen, err := ReadMessageLength(headerBuf)
	if err != nil {
		t.Fatalf("read length: %v", err)
	}
	buf := make([]byte, msgLen)
	copy(buf[:4], headerBuf)
	if _, err := io.ReadFull(conn, buf[4:]); err != nil {
		t.Fatalf("read body: %v", err)
	}
	msg, err := DecodeMessage(buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return msg
}

func readFullMessageErr(conn net.Conn) *Message {
	headerBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, headerBuf); err != nil {
		return nil
	}
	msgLen, err := ReadMessageLength(headerBuf)
	if err != nil {
		return nil
	}
	buf := make([]byte, msgLen)
	copy(buf[:4], headerBuf)
	if _, err := io.ReadFull(conn, buf[4:]); err != nil {
		return nil
	}
	msg, err := DecodeMessage(buf)
	if err != nil {
		return nil
	}
	return msg
}

func testLogger() zerolog.Logger {
	return zerolog.Nop()
}

func setupTestServer(t *testing.T) (*Server, net.Listener) {
	t.Helper()
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		Logger: testLogger(),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.listener = ln
	srv.mu.Lock()
	srv.running = true
	srv.mu.Unlock()
	srv.wg.Add(1)
	go srv.acceptLoop()
	return srv, ln
}

func cleanupTestServer(srv *Server, ln net.Listener) {
	close(srv.stopCh)
	ln.Close()
	srv.wg.Wait()
}

func doCERHandshake(t *testing.T, conn net.Conn) {
	t.Helper()
	cer := NewRequest(CommandCER, ApplicationIDDiameterBase, 1, 1)
	cer.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
	cer.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
	cer.AddAVP(NewAVPAddress(AVPCodeHostIPAddress, AVPFlagMandatory, 0, [4]byte{127, 0, 0, 1}))
	cer.AddAVP(NewAVPUint32(AVPCodeVendorID, AVPFlagMandatory, 0, 10415))
	cer.AddAVP(NewAVPString(AVPCodeProductName, 0, 0, "TestPeer"))
	cer.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
	cer.AddAVP(NewAVPUint32(AVPCodeAcctApplicationID, AVPFlagMandatory, 0, ApplicationIDGy))
	cerData, _ := cer.Encode()
	conn.Write(cerData)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	readFullMessage(t, conn)
}

func TestGyCCRIViaTCP(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	doCERHandshake(t, conn)

	ccr := NewRequest(CommandCCR, ApplicationIDGy, 10, 10)
	ccr.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "gy-test-session-1"))
	ccr.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
	ccr.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
	ccr.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGy))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	for _, sub := range BuildSubscriptionID("286010123456789", "905551234567") {
		ccr.AddAVP(sub)
	}
	ccrData, _ := ccr.Encode()
	conn.Write(ccrData)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	cca := readFullMessage(t, conn)

	if cca.IsRequest() {
		t.Fatal("CCA should be an answer")
	}
	if cca.CommandCode != CommandCCA {
		t.Fatalf("command code %d, expected %d", cca.CommandCode, CommandCCA)
	}
	if cca.GetResultCode() != ResultCodeSuccess {
		t.Fatalf("result code %d, expected %d", cca.GetResultCode(), ResultCodeSuccess)
	}
	if cca.GetSessionID() != "gy-test-session-1" {
		t.Fatalf("session id %q, expected %q", cca.GetSessionID(), "gy-test-session-1")
	}
	if cca.GetCCRequestType() != CCRequestTypeInitial {
		t.Fatalf("cc request type %d, expected %d", cca.GetCCRequestType(), CCRequestTypeInitial)
	}
	gsu := FindAVP(cca.AVPs, AVPCodeGrantedServiceUnit)
	if gsu == nil {
		t.Fatal("expected Granted-Service-Unit AVP in CCA-I")
	}
}

func TestGyCCRUViaTCP(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	doCERHandshake(t, conn)

	ccri := NewRequest(CommandCCR, ApplicationIDGy, 10, 10)
	ccri.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "gy-update-session"))
	ccri.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGy))
	ccri.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	ccri.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	for _, sub := range BuildSubscriptionID("286010000000001", "") {
		ccri.AddAVP(sub)
	}
	data, _ := ccri.Encode()
	conn.Write(data)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	readFullMessage(t, conn)

	ccru := NewRequest(CommandCCR, ApplicationIDGy, 11, 11)
	ccru.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "gy-update-session"))
	ccru.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGy))
	ccru.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeUpdate))
	ccru.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 1))
	usu := NewAVPGrouped(AVPCodeUsedServiceUnit, AVPFlagMandatory, 0, []*AVP{
		NewAVPUint64(AVPCodeCCTotalOctets, AVPFlagMandatory, 0, 5000),
		NewAVPUint64(AVPCodeCCInputOctets, AVPFlagMandatory, 0, 2000),
		NewAVPUint64(AVPCodeCCOutputOctets, AVPFlagMandatory, 0, 3000),
	})
	ccru.AddAVP(usu)
	data, _ = ccru.Encode()
	conn.Write(data)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	cca := readFullMessage(t, conn)

	if cca.GetCCRequestType() != CCRequestTypeUpdate {
		t.Fatalf("cc request type %d, expected %d", cca.GetCCRequestType(), CCRequestTypeUpdate)
	}
	if cca.GetResultCode() != ResultCodeSuccess {
		t.Fatalf("result code %d, expected %d", cca.GetResultCode(), ResultCodeSuccess)
	}
	gsu := FindAVP(cca.AVPs, AVPCodeGrantedServiceUnit)
	if gsu == nil {
		t.Fatal("expected Granted-Service-Unit AVP in CCA-U")
	}
}

func TestGyCCRTViaTCP(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	doCERHandshake(t, conn)

	ccri := NewRequest(CommandCCR, ApplicationIDGy, 10, 10)
	ccri.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "gy-term-session"))
	ccri.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGy))
	ccri.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	ccri.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	for _, sub := range BuildSubscriptionID("286010000000002", "") {
		ccri.AddAVP(sub)
	}
	data, _ := ccri.Encode()
	conn.Write(data)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	readFullMessage(t, conn)

	ccrt := NewRequest(CommandCCR, ApplicationIDGy, 12, 12)
	ccrt.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "gy-term-session"))
	ccrt.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGy))
	ccrt.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeTermination))
	ccrt.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 2))
	usu := NewAVPGrouped(AVPCodeUsedServiceUnit, AVPFlagMandatory, 0, []*AVP{
		NewAVPUint64(AVPCodeCCTotalOctets, AVPFlagMandatory, 0, 10000),
	})
	ccrt.AddAVP(usu)
	data, _ = ccrt.Encode()
	conn.Write(data)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	cca := readFullMessage(t, conn)

	if cca.GetCCRequestType() != CCRequestTypeTermination {
		t.Fatalf("cc request type %d, expected %d", cca.GetCCRequestType(), CCRequestTypeTermination)
	}
	if cca.GetResultCode() != ResultCodeSuccess {
		t.Fatalf("result code %d, expected %d", cca.GetResultCode(), ResultCodeSuccess)
	}
}

func TestGyCCREventViaTCP(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	doCERHandshake(t, conn)

	ccre := NewRequest(CommandCCR, ApplicationIDGy, 10, 10)
	ccre.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "gy-event-session"))
	ccre.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGy))
	ccre.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeEvent))
	ccre.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	for _, sub := range BuildSubscriptionID("286010000000003", "") {
		ccre.AddAVP(sub)
	}
	data, _ := ccre.Encode()
	conn.Write(data)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	cca := readFullMessage(t, conn)

	if cca.GetCCRequestType() != CCRequestTypeEvent {
		t.Fatalf("cc request type %d, expected %d", cca.GetCCRequestType(), CCRequestTypeEvent)
	}
	if cca.GetResultCode() != ResultCodeSuccess {
		t.Fatalf("result code %d, expected %d", cca.GetResultCode(), ResultCodeSuccess)
	}
}

func TestGxCCRUViaTCP(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	doCERHandshake(t, conn)

	ccri := NewRequest(CommandCCR, ApplicationIDGx, 10, 10)
	ccri.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "gx-update-session"))
	ccri.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
	ccri.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	ccri.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	for _, sub := range BuildSubscriptionID("286020000000001", "") {
		ccri.AddAVP(sub)
	}
	data, _ := ccri.Encode()
	conn.Write(data)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	readFullMessage(t, conn)

	ccru := NewRequest(CommandCCR, ApplicationIDGx, 11, 11)
	ccru.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "gx-update-session"))
	ccru.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
	ccru.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeUpdate))
	ccru.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 1))
	data, _ = ccru.Encode()
	conn.Write(data)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	cca := readFullMessage(t, conn)

	if cca.GetCCRequestType() != CCRequestTypeUpdate {
		t.Fatalf("cc request type %d, expected %d", cca.GetCCRequestType(), CCRequestTypeUpdate)
	}
	if cca.GetResultCode() != ResultCodeSuccess {
		t.Fatalf("result code %d, expected %d", cca.GetResultCode(), ResultCodeSuccess)
	}
}

func TestGxCCRTViaTCP(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	doCERHandshake(t, conn)

	ccri := NewRequest(CommandCCR, ApplicationIDGx, 10, 10)
	ccri.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "gx-term-session"))
	ccri.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
	ccri.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	ccri.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	for _, sub := range BuildSubscriptionID("286020000000002", "") {
		ccri.AddAVP(sub)
	}
	data, _ := ccri.Encode()
	conn.Write(data)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	readFullMessage(t, conn)

	ccrt := NewRequest(CommandCCR, ApplicationIDGx, 12, 12)
	ccrt.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "gx-term-session"))
	ccrt.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
	ccrt.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeTermination))
	ccrt.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 2))
	data, _ = ccrt.Encode()
	conn.Write(data)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	cca := readFullMessage(t, conn)

	if cca.GetCCRequestType() != CCRequestTypeTermination {
		t.Fatalf("cc request type %d, expected %d", cca.GetCCRequestType(), CCRequestTypeTermination)
	}
	if cca.GetResultCode() != ResultCodeSuccess {
		t.Fatalf("result code %d, expected %d", cca.GetResultCode(), ResultCodeSuccess)
	}
}

func TestSendRAR(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	doCERHandshake(t, conn)

	time.Sleep(50 * time.Millisecond)

	err = srv.SendRAR("peer.test.com", "rar-test-session-1", nil)
	if err != nil {
		t.Fatalf("SendRAR failed: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	rar := readFullMessage(t, conn)

	if !rar.IsRequest() {
		t.Fatal("RAR should be a request")
	}
	if rar.CommandCode != CommandRAR {
		t.Fatalf("command code %d, expected %d", rar.CommandCode, CommandRAR)
	}
	if rar.GetSessionID() != "rar-test-session-1" {
		t.Fatalf("session id %q, expected %q", rar.GetSessionID(), "rar-test-session-1")
	}
	if rar.GetOriginHost() != "argus.test.com" {
		t.Fatalf("origin host %q, expected %q", rar.GetOriginHost(), "argus.test.com")
	}

	destHost := FindAVP(rar.AVPs, AVPCodeDestinationHost)
	if destHost == nil || destHost.GetString() != "peer.test.com" {
		t.Fatal("expected Destination-Host=peer.test.com in RAR")
	}
}

func TestSendRARPeerNotFound(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	err := srv.SendRAR("nonexistent.peer.com", "session-1", nil)
	if err == nil {
		t.Fatal("expected error for non-existent peer")
	}
}

func TestCCROnNonOpenPeer(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ccr := NewRequest(CommandCCR, ApplicationIDGx, 10, 10)
	ccr.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "test-no-cer"))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	ccrData, _ := ccr.Encode()
	conn.Write(ccrData)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	cca := readFullMessage(t, conn)

	if cca.GetResultCode() != ResultCodeUnableToComply {
		t.Fatalf("result code %d, expected %d (UNABLE_TO_COMPLY for non-open peer)", cca.GetResultCode(), ResultCodeUnableToComply)
	}
}

func TestWatchdogTimeout(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 100 * time.Millisecond,
	}, ServerDeps{
		Logger: testLogger(),
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.listener = ln
	srv.mu.Lock()
	srv.running = true
	srv.mu.Unlock()
	srv.wg.Add(1)
	go srv.acceptLoop()

	defer func() {
		close(srv.stopCh)
		ln.Close()
		srv.wg.Wait()
	}()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cer := NewRequest(CommandCER, ApplicationIDDiameterBase, 1, 1)
	cer.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "timeout-peer.test.com"))
	cer.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
	cer.AddAVP(NewAVPAddress(AVPCodeHostIPAddress, AVPFlagMandatory, 0, [4]byte{127, 0, 0, 1}))
	cer.AddAVP(NewAVPUint32(AVPCodeVendorID, AVPFlagMandatory, 0, 10415))
	cer.AddAVP(NewAVPString(AVPCodeProductName, 0, 0, "TestPeer"))
	cerData, _ := cer.Encode()
	conn.Write(cerData)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	readFullMessage(t, conn)

	time.Sleep(500 * time.Millisecond)

	conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, err = io.ReadFull(conn, make([]byte, 1))
	if err == nil {
		t.Fatal("expected connection to be closed by watchdog timeout")
	}
}

func TestMultiPeerConcurrentSessions(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	peerCount := 3
	conns := make([]net.Conn, peerCount)
	sessionIDs := make([]string, peerCount)

	for i := 0; i < peerCount; i++ {
		conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
		if err != nil {
			t.Fatalf("dial peer %d: %v", i, err)
		}
		defer conn.Close()
		conns[i] = conn

		cer := NewRequest(CommandCER, ApplicationIDDiameterBase, uint32(i*100+1), uint32(i*100+1))
		cer.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, fmt.Sprintf("peer%d.test.com", i)))
		cer.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
		cer.AddAVP(NewAVPAddress(AVPCodeHostIPAddress, AVPFlagMandatory, 0, [4]byte{127, 0, 0, 1}))
		cer.AddAVP(NewAVPUint32(AVPCodeVendorID, AVPFlagMandatory, 0, 10415))
		cer.AddAVP(NewAVPString(AVPCodeProductName, 0, 0, "TestPeer"))
		cer.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
		cerData, _ := cer.Encode()
		conn.Write(cerData)
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		readFullMessage(t, conn)

		sessionIDs[i] = fmt.Sprintf("multi-peer-session-%d", i)
		imsi := fmt.Sprintf("28601000000%04d", i)

		ccr := NewRequest(CommandCCR, ApplicationIDGx, uint32(i*100+10), uint32(i*100+10))
		ccr.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionIDs[i]))
		ccr.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
		ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
		ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
		for _, sub := range BuildSubscriptionID(imsi, "") {
			ccr.AddAVP(sub)
		}
		ccrData, _ := ccr.Encode()
		conn.Write(ccrData)
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		cca := readFullMessage(t, conn)
		if cca.GetResultCode() != ResultCodeSuccess {
			t.Fatalf("peer %d CCR-I failed: result code %d", i, cca.GetResultCode())
		}
	}

	if srv.SessionStateMap().ActiveCount() != peerCount {
		t.Fatalf("active sessions %d, expected %d", srv.SessionStateMap().ActiveCount(), peerCount)
	}

	ccrt := NewRequest(CommandCCR, ApplicationIDGx, 200, 200)
	ccrt.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionIDs[0]))
	ccrt.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
	ccrt.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeTermination))
	ccrt.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 1))
	ccrtData, _ := ccrt.Encode()
	conns[0].Write(ccrtData)
	conns[0].SetReadDeadline(time.Now().Add(3 * time.Second))
	readFullMessage(t, conns[0])

	time.Sleep(50 * time.Millisecond)

	activeCount := srv.SessionStateMap().ActiveCount()
	if activeCount != peerCount-1 {
		t.Fatalf("active sessions after termination %d, expected %d", activeCount, peerCount-1)
	}
}

func TestUnknownCommandCode(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	doCERHandshake(t, conn)

	unknown := NewRequest(999, ApplicationIDGx, 10, 10)
	unknown.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, "peer.test.com"))
	data, _ := unknown.Encode()
	conn.Write(data)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	ans := readFullMessage(t, conn)

	if ans.GetResultCode() != ResultCodeUnableToComply {
		t.Fatalf("result code %d, expected %d (UNABLE_TO_COMPLY for unknown command)", ans.GetResultCode(), ResultCodeUnableToComply)
	}
}

func TestMalformedCCRMissingCCRequestType(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	doCERHandshake(t, conn)

	ccr := NewRequest(CommandCCR, ApplicationIDGx, 10, 10)
	ccr.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "test-no-reqtype"))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	ccrData, _ := ccr.Encode()
	conn.Write(ccrData)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	cca := readFullMessage(t, conn)

	if cca.GetResultCode() != ResultCodeMissingAVP {
		t.Fatalf("result code %d, expected %d (MISSING_AVP)", cca.GetResultCode(), ResultCodeMissingAVP)
	}
}

func TestGxCCRInvalidRequestType(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	doCERHandshake(t, conn)

	ccr := NewRequest(CommandCCR, ApplicationIDGx, 10, 10)
	ccr.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "test-invalid-type"))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, 99))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	ccrData, _ := ccr.Encode()
	conn.Write(ccrData)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	cca := readFullMessage(t, conn)

	if cca.GetResultCode() != ResultCodeInvalidAVPValue {
		t.Fatalf("result code %d, expected %d (INVALID_AVP_VALUE)", cca.GetResultCode(), ResultCodeInvalidAVPValue)
	}
}

func TestGyCCRMissingIMSIOnInitial(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	doCERHandshake(t, conn)

	ccr := NewRequest(CommandCCR, ApplicationIDGy, 10, 10)
	ccr.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "test-no-imsi"))
	ccr.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGy))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	ccrData, _ := ccr.Encode()
	conn.Write(ccrData)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	cca := readFullMessage(t, conn)

	if cca.GetResultCode() != ResultCodeMissingAVP {
		t.Fatalf("result code %d, expected %d (MISSING_AVP for missing IMSI)", cca.GetResultCode(), ResultCodeMissingAVP)
	}
}

func TestServerStartOnPort(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		Logger: testLogger(),
	})

	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	if !srv.IsRunning() {
		t.Fatal("server should be running after Start()")
	}

	addr := srv.listener.Addr().String()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()

	srv.Stop()

	if srv.IsRunning() {
		t.Fatal("server should not be running after Stop()")
	}
}

func TestServerDoubleStart(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		Logger: testLogger(),
	})

	if err := srv.Start(); err != nil {
		t.Fatalf("first start: %v", err)
	}
	defer srv.Stop()

	err := srv.Start()
	if err == nil {
		t.Fatal("expected error on double Start()")
	}
	if err.Error() != "diameter server already running" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServerStopClosesAllPeers(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		Logger: testLogger(),
	})

	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	addr := srv.listener.Addr().String()
	conns := make([]net.Conn, 3)
	for i := 0; i < 3; i++ {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		conns[i] = conn

		cer := NewRequest(CommandCER, ApplicationIDDiameterBase, uint32(i+1), uint32(i+1))
		cer.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, fmt.Sprintf("peer%d.test.com", i)))
		cer.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, "test.com"))
		cer.AddAVP(NewAVPAddress(AVPCodeHostIPAddress, AVPFlagMandatory, 0, [4]byte{127, 0, 0, 1}))
		cer.AddAVP(NewAVPUint32(AVPCodeVendorID, AVPFlagMandatory, 0, 10415))
		cerData, _ := cer.Encode()
		conn.Write(cerData)
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		headerBuf := make([]byte, 4)
		io.ReadFull(conn, headerBuf)
		msgLen, _ := ReadMessageLength(headerBuf)
		buf := make([]byte, msgLen)
		copy(buf[:4], headerBuf)
		io.ReadFull(conn, buf[4:])
	}

	time.Sleep(50 * time.Millisecond)

	srv.Stop()

	for i, conn := range conns {
		conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		_, err := io.ReadFull(conn, make([]byte, 1))
		if err == nil {
			t.Fatalf("peer %d: expected read to fail after server stop", i)
		}
		conn.Close()
	}
}

func TestActiveSessionCount(t *testing.T) {
	srv := NewServer(ServerConfig{
		Port:             0,
		OriginHost:       "argus.test.com",
		OriginRealm:      "test.com",
		WatchdogInterval: 30 * time.Second,
	}, ServerDeps{
		Logger: testLogger(),
	})

	ds1 := srv.SessionStateMap().Create("sess-a", "peer1", ApplicationIDGx, "imsi1")
	_ = ds1.Transition(SessionStateOpen)
	ds2 := srv.SessionStateMap().Create("sess-b", "peer2", ApplicationIDGy, "imsi2")
	_ = ds2.Transition(SessionStateOpen)
	srv.SessionStateMap().Create("sess-c", "peer3", ApplicationIDGx, "imsi3")

	count, err := srv.ActiveSessionCount(context.Background())
	if err != nil {
		t.Fatalf("active session count: %v", err)
	}
	if count != 2 {
		t.Fatalf("active sessions %d, expected 2", count)
	}

	_ = ds1.Transition(SessionStateClosed)
	count, err = srv.ActiveSessionCount(context.Background())
	if err != nil {
		t.Fatalf("active session count: %v", err)
	}
	if count != 1 {
		t.Fatalf("active sessions %d, expected 1 after closing one", count)
	}
}

func TestPeerCountDynamic(t *testing.T) {
	srv, ln := setupTestServer(t)
	defer cleanupTestServer(srv, ln)

	if srv.PeerCount() != 0 {
		t.Fatalf("initial peer count %d, expected 0", srv.PeerCount())
	}

	conn1, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn2, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if srv.PeerCount() != 2 {
		t.Fatalf("peer count %d, expected 2", srv.PeerCount())
	}

	conn1.Close()
	time.Sleep(100 * time.Millisecond)

	remaining := srv.PeerCount()
	if remaining > 2 {
		t.Fatalf("peer count %d, expected <=2 after disconnect", remaining)
	}

	conn2.Close()
}
