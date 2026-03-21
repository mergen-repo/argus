package ota

import (
	"encoding/json"
	"testing"
)

func TestAPDU_Bytes_Header(t *testing.T) {
	apdu := &APDU{CLA: 0x00, INS: 0xA4, P1: 0x00, P2: 0x04}
	b := apdu.Bytes()

	if len(b) != 4 {
		t.Fatalf("len = %d, want 4", len(b))
	}
	if b[0] != 0x00 || b[1] != 0xA4 || b[2] != 0x00 || b[3] != 0x04 {
		t.Errorf("header = %x, want 00A40004", b)
	}
}

func TestAPDU_Bytes_WithData(t *testing.T) {
	data := []byte{0x3F, 0x00}
	apdu := &APDU{CLA: 0x00, INS: 0xA4, P1: 0x00, P2: 0x04, Data: data}
	b := apdu.Bytes()

	if len(b) != 7 {
		t.Fatalf("len = %d, want 7 (4 header + 1 Lc + 2 data)", len(b))
	}
	if b[4] != 0x02 {
		t.Errorf("Lc = %02x, want 02", b[4])
	}
	if b[5] != 0x3F || b[6] != 0x00 {
		t.Errorf("data = %x, want 3F00", b[5:])
	}
}

func TestAPDU_Bytes_WithLe(t *testing.T) {
	le := byte(0x10)
	apdu := &APDU{CLA: 0x00, INS: 0xB0, P1: 0x00, P2: 0x00, Le: &le}
	b := apdu.Bytes()

	if len(b) != 5 {
		t.Fatalf("len = %d, want 5 (4 header + 1 Le)", len(b))
	}
	if b[4] != 0x10 {
		t.Errorf("Le = %02x, want 10", b[4])
	}
}

func TestAPDU_Bytes_WithDataAndLe(t *testing.T) {
	data := []byte{0x01, 0x02}
	le := byte(0xFF)
	apdu := &APDU{CLA: 0x00, INS: 0xB0, P1: 0x00, P2: 0x00, Data: data, Le: &le}
	b := apdu.Bytes()

	if len(b) != 8 {
		t.Fatalf("len = %d, want 8 (4 header + 1 Lc + 2 data + 1 Le)", len(b))
	}
}

func TestBuildAPDU_UpdateFile(t *testing.T) {
	payload := json.RawMessage(`{"file_id":"3F00","offset":0,"content":"AQID"}`)
	result, err := BuildAPDU(CmdUpdateFile, payload)
	if err != nil {
		t.Fatalf("BuildAPDU: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected non-empty APDU bytes")
	}
	if result[1] != insSelect {
		t.Errorf("first APDU INS = %02x, want %02x (SELECT)", result[1], insSelect)
	}
}

func TestBuildAPDU_UpdateFile_EmptyContent(t *testing.T) {
	payload := json.RawMessage(`{"file_id":"3F00","offset":0,"content":""}`)
	_, err := BuildAPDU(CmdUpdateFile, payload)
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestBuildAPDU_UpdateFile_ContentTooLong(t *testing.T) {
	bigContent := make([]byte, 256)
	for i := range bigContent {
		bigContent[i] = byte(i % 256)
	}
	p := UpdateFilePayload{FileID: "3F00", Offset: 0, Content: bigContent}
	raw, _ := json.Marshal(p)
	_, err := BuildAPDU(CmdUpdateFile, raw)
	if err == nil {
		t.Error("expected error for content > 255 bytes")
	}
}

func TestBuildAPDU_InstallApplet(t *testing.T) {
	p := InstallAppletPayload{
		PackageAID: []byte{0xA0, 0x00, 0x00, 0x00},
		AppletAID:  []byte{0xA0, 0x00, 0x00, 0x01},
	}
	raw, _ := json.Marshal(p)
	result, err := BuildAPDU(CmdInstallApplet, raw)
	if err != nil {
		t.Fatalf("BuildAPDU: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected non-empty APDU bytes")
	}
	if result[0] != claGP {
		t.Errorf("CLA = %02x, want %02x (GlobalPlatform)", result[0], claGP)
	}
	if result[1] != insInstall {
		t.Errorf("INS = %02x, want %02x (INSTALL)", result[1], insInstall)
	}
}

func TestBuildAPDU_InstallApplet_MissingPackageAID(t *testing.T) {
	p := InstallAppletPayload{AppletAID: []byte{0xA0}}
	raw, _ := json.Marshal(p)
	_, err := BuildAPDU(CmdInstallApplet, raw)
	if err == nil {
		t.Error("expected error for missing package_aid")
	}
}

func TestBuildAPDU_InstallApplet_MissingAppletAID(t *testing.T) {
	p := InstallAppletPayload{PackageAID: []byte{0xA0}}
	raw, _ := json.Marshal(p)
	_, err := BuildAPDU(CmdInstallApplet, raw)
	if err == nil {
		t.Error("expected error for missing applet_aid")
	}
}

func TestBuildAPDU_InstallApplet_WithInstance(t *testing.T) {
	p := InstallAppletPayload{
		PackageAID: []byte{0xA0, 0x00},
		AppletAID:  []byte{0xA0, 0x01},
		Instance:   []byte{0xA0, 0x02},
	}
	raw, _ := json.Marshal(p)
	result, err := BuildAPDU(CmdInstallApplet, raw)
	if err != nil {
		t.Fatalf("BuildAPDU: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected non-empty APDU bytes")
	}
}

func TestBuildAPDU_DeleteApplet(t *testing.T) {
	p := DeleteAppletPayload{AID: []byte{0xA0, 0x00, 0x00, 0x01}}
	raw, _ := json.Marshal(p)
	result, err := BuildAPDU(CmdDeleteApplet, raw)
	if err != nil {
		t.Fatalf("BuildAPDU: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected non-empty APDU bytes")
	}
	if result[0] != claGP {
		t.Errorf("CLA = %02x, want %02x", result[0], claGP)
	}
	if result[1] != insDelete {
		t.Errorf("INS = %02x, want %02x (DELETE)", result[1], insDelete)
	}
}

func TestBuildAPDU_DeleteApplet_EmptyAID(t *testing.T) {
	p := DeleteAppletPayload{AID: nil}
	raw, _ := json.Marshal(p)
	_, err := BuildAPDU(CmdDeleteApplet, raw)
	if err == nil {
		t.Error("expected error for empty AID")
	}
}

func TestBuildAPDU_ReadFile(t *testing.T) {
	p := ReadFilePayload{FileID: "7FFF", Offset: 0, Length: 16}
	raw, _ := json.Marshal(p)
	result, err := BuildAPDU(CmdReadFile, raw)
	if err != nil {
		t.Fatalf("BuildAPDU: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected non-empty APDU bytes")
	}
}

func TestBuildAPDU_ReadFile_DefaultLength(t *testing.T) {
	p := ReadFilePayload{FileID: "3F00", Offset: 0, Length: 0}
	raw, _ := json.Marshal(p)
	result, err := BuildAPDU(CmdReadFile, raw)
	if err != nil {
		t.Fatalf("BuildAPDU: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected non-empty APDU bytes")
	}
}

func TestBuildAPDU_SIMToolkit(t *testing.T) {
	p := SIMToolkitPayload{Tag: 0xD0, Data: []byte{0x01, 0x02}}
	raw, _ := json.Marshal(p)
	result, err := BuildAPDU(CmdSIMToolkit, raw)
	if err != nil {
		t.Fatalf("BuildAPDU: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected non-empty APDU bytes")
	}
	if result[1] != insEnvelope {
		t.Errorf("INS = %02x, want %02x (ENVELOPE)", result[1], insEnvelope)
	}
}

func TestBuildAPDU_InvalidCommandType(t *testing.T) {
	payload := json.RawMessage(`{}`)
	_, err := BuildAPDU(CommandType("UNKNOWN"), payload)
	if err == nil {
		t.Error("expected error for unknown command type")
	}
}

func TestBuildAPDU_InvalidJSON(t *testing.T) {
	_, err := BuildAPDU(CmdUpdateFile, json.RawMessage(`{broken`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestHexCharVal(t *testing.T) {
	tests := []struct {
		input byte
		want  byte
	}{
		{'0', 0}, {'5', 5}, {'9', 9},
		{'a', 10}, {'f', 15},
		{'A', 10}, {'F', 15},
		{'g', 0}, {'z', 0},
	}
	for _, tt := range tests {
		got := hexCharVal(tt.input)
		if got != tt.want {
			t.Errorf("hexCharVal(%c) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestBuildSelectFileAPDU(t *testing.T) {
	apdu := buildSelectFileAPDU("3F00")
	b := apdu.Bytes()

	if b[0] != claSIM {
		t.Errorf("CLA = %02x, want %02x", b[0], claSIM)
	}
	if b[1] != insSelect {
		t.Errorf("INS = %02x, want %02x", b[1], insSelect)
	}
	if b[4] != 0x02 {
		t.Errorf("Lc = %02x, want 02", b[4])
	}
	if b[5] != 0x3F || b[6] != 0x00 {
		t.Errorf("file ID = %x, want 3F00", b[5:7])
	}
}
