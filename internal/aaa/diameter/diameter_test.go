package diameter

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

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
		{9999, "unknown_9999"},
	}
	for _, tt := range tests {
		if got := mapDiameterRATType(tt.input); got != tt.want {
			t.Errorf("mapDiameterRATType(%d) = %q, want %q", tt.input, got, tt.want)
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

func testLogger() zerolog.Logger {
	return zerolog.Nop()
}
