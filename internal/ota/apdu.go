package ota

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
)

const (
	claSIM       byte = 0x00
	claGP        byte = 0x80
	insSelect    byte = 0xA4
	insUpdateBin byte = 0xD6
	insReadBin   byte = 0xB0
	insInstall   byte = 0xE6
	insDelete    byte = 0xE4
	insEnvelope  byte = 0xC2

	installForLoad    byte = 0x02
	installForInstall byte = 0x0C
	deleteObject      byte = 0x00
)

type APDU struct {
	CLA  byte
	INS  byte
	P1   byte
	P2   byte
	Data []byte
	Le   *byte
}

func (a *APDU) Bytes() []byte {
	buf := []byte{a.CLA, a.INS, a.P1, a.P2}
	if len(a.Data) > 0 {
		buf = append(buf, byte(len(a.Data)))
		buf = append(buf, a.Data...)
	}
	if a.Le != nil {
		buf = append(buf, *a.Le)
	}
	return buf
}

type UpdateFilePayload struct {
	FileID  string `json:"file_id"`
	Offset  int    `json:"offset"`
	Content []byte `json:"content"`
}

type InstallAppletPayload struct {
	PackageAID []byte `json:"package_aid"`
	AppletAID  []byte `json:"applet_aid"`
	Instance   []byte `json:"instance_aid"`
}

type DeleteAppletPayload struct {
	AID []byte `json:"aid"`
}

type ReadFilePayload struct {
	FileID string `json:"file_id"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

type SIMToolkitPayload struct {
	Tag  byte   `json:"tag"`
	Data []byte `json:"data"`
}

func BuildAPDU(cmdType CommandType, payload json.RawMessage) ([]byte, error) {
	switch cmdType {
	case CmdUpdateFile:
		return buildUpdateFileAPDU(payload)
	case CmdInstallApplet:
		return buildInstallAppletAPDU(payload)
	case CmdDeleteApplet:
		return buildDeleteAppletAPDU(payload)
	case CmdReadFile:
		return buildReadFileAPDU(payload)
	case CmdSIMToolkit:
		return buildSIMToolkitAPDU(payload)
	default:
		return nil, fmt.Errorf("unsupported command type: %s", cmdType)
	}
}

func buildUpdateFileAPDU(raw json.RawMessage) ([]byte, error) {
	var p UpdateFilePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("unmarshal update_file payload: %w", err)
	}
	if len(p.Content) == 0 {
		return nil, fmt.Errorf("update_file: content is required")
	}
	if len(p.Content) > 255 {
		return nil, fmt.Errorf("update_file: content exceeds 255 bytes")
	}

	selectAPDU := buildSelectFileAPDU(p.FileID)

	offset := make([]byte, 2)
	binary.BigEndian.PutUint16(offset, uint16(p.Offset))

	updateAPDU := &APDU{
		CLA:  claSIM,
		INS:  insUpdateBin,
		P1:   offset[0],
		P2:   offset[1],
		Data: p.Content,
	}

	result := append(selectAPDU.Bytes(), updateAPDU.Bytes()...)
	return result, nil
}

func buildInstallAppletAPDU(raw json.RawMessage) ([]byte, error) {
	var p InstallAppletPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("unmarshal install_applet payload: %w", err)
	}
	if len(p.PackageAID) == 0 {
		return nil, fmt.Errorf("install_applet: package_aid is required")
	}
	if len(p.AppletAID) == 0 {
		return nil, fmt.Errorf("install_applet: applet_aid is required")
	}

	installData := []byte{byte(len(p.PackageAID))}
	installData = append(installData, p.PackageAID...)
	installData = append(installData, byte(len(p.AppletAID)))
	installData = append(installData, p.AppletAID...)

	instanceAID := p.Instance
	if len(instanceAID) == 0 {
		instanceAID = p.AppletAID
	}
	installData = append(installData, byte(len(instanceAID)))
	installData = append(installData, instanceAID...)

	installData = append(installData, 0x01, 0x00, 0x02, 0xC9, 0x00)

	apdu := &APDU{
		CLA:  claGP,
		INS:  insInstall,
		P1:   installForInstall,
		P2:   0x00,
		Data: installData,
	}

	return apdu.Bytes(), nil
}

func buildDeleteAppletAPDU(raw json.RawMessage) ([]byte, error) {
	var p DeleteAppletPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("unmarshal delete_applet payload: %w", err)
	}
	if len(p.AID) == 0 {
		return nil, fmt.Errorf("delete_applet: aid is required")
	}

	deleteData := []byte{0x4F, byte(len(p.AID))}
	deleteData = append(deleteData, p.AID...)

	apdu := &APDU{
		CLA:  claGP,
		INS:  insDelete,
		P1:   deleteObject,
		P2:   0x00,
		Data: deleteData,
	}

	return apdu.Bytes(), nil
}

func buildReadFileAPDU(raw json.RawMessage) ([]byte, error) {
	var p ReadFilePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("unmarshal read_file payload: %w", err)
	}
	if p.Length <= 0 || p.Length > 256 {
		p.Length = 256
	}

	selectAPDU := buildSelectFileAPDU(p.FileID)

	offset := make([]byte, 2)
	binary.BigEndian.PutUint16(offset, uint16(p.Offset))

	le := byte(p.Length & 0xFF)
	readAPDU := &APDU{
		CLA: claSIM,
		INS: insReadBin,
		P1:  offset[0],
		P2:  offset[1],
		Le:  &le,
	}

	result := append(selectAPDU.Bytes(), readAPDU.Bytes()...)
	return result, nil
}

func buildSIMToolkitAPDU(raw json.RawMessage) ([]byte, error) {
	var p SIMToolkitPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("unmarshal sim_toolkit payload: %w", err)
	}

	envelopeData := []byte{p.Tag, byte(len(p.Data))}
	envelopeData = append(envelopeData, p.Data...)

	apdu := &APDU{
		CLA:  claSIM,
		INS:  insEnvelope,
		P1:   0x00,
		P2:   0x00,
		Data: envelopeData,
	}

	return apdu.Bytes(), nil
}

func buildSelectFileAPDU(fileID string) *APDU {
	fid := []byte(fileID)
	if len(fileID) >= 2 {
		fid = make([]byte, len(fileID)/2)
		for i := 0; i < len(fileID)/2; i++ {
			hi := hexCharVal(fileID[i*2])
			lo := hexCharVal(fileID[i*2+1])
			fid[i] = (hi << 4) | lo
		}
	}

	return &APDU{
		CLA:  claSIM,
		INS:  insSelect,
		P1:   0x00,
		P2:   0x04,
		Data: fid,
	}
}

func hexCharVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}
