package sba

import "time"

type AuthType string

const (
	AuthType5GAKA  AuthType = "5G_AKA"
	AuthTypeEAPAKA AuthType = "EAP_AKA_PRIME"
)

type SNSSAI struct {
	SST int    `json:"sst"`
	SD  string `json:"sd,omitempty"`
}

type PlmnID struct {
	MCC string `json:"mcc"`
	MNC string `json:"mnc"`
}

type GUAMI struct {
	PlmnID PlmnID `json:"plmnId"`
	AmfID  string `json:"amfId"`
}

type AuthenticationRequest struct {
	SUPIOrSUCI            string      `json:"supiOrSuci"`
	ServingNetworkName    string      `json:"servingNetworkName"`
	RequestedNSSAI        []SNSSAI    `json:"requestedNssai,omitempty"`
	ResynchronizationInfo *ResyncInfo `json:"resynchronizationInfo,omitempty"`
	PEI                   string      `json:"pei,omitempty"`
}

type ResyncInfo struct {
	RAND string `json:"rand"`
	AUTS string `json:"auts"`
}

type AKA5GAuthData struct {
	RAND      string `json:"rand"`
	AUTN      string `json:"autn"`
	HxresStar string `json:"hxresStar"`
}

type AuthLink struct {
	Href string `json:"href"`
}

type AuthenticationResponse struct {
	AuthType   AuthType            `json:"authType"`
	AuthData5G *AKA5GAuthData      `json:"5gAuthData,omitempty"`
	Links      map[string]AuthLink `json:"_links,omitempty"`
	SUPI       string              `json:"supi,omitempty"`
}

type ConfirmationRequest struct {
	ResStar string `json:"resStar"`
}

type ConfirmationResponse struct {
	AuthResult string `json:"authResult"`
	SUPI       string `json:"supi,omitempty"`
	Kseaf      string `json:"kseaf,omitempty"`
}

type SecurityInfoRequest struct {
	ServingNetworkName string `json:"servingNetworkName"`
	AusfInstanceID     string `json:"ausfInstanceId"`
}

type AuthVector5G struct {
	AvType   AuthType `json:"avType"`
	RAND     string   `json:"rand"`
	AUTN     string   `json:"autn"`
	XresStar string   `json:"xresStar"`
	Kausf    string   `json:"kausf"`
}

type SecurityInfoResponse struct {
	AuthVector *AuthVector5G `json:"authenticationVector"`
	SUPI       string        `json:"supi"`
}

type AuthEvent struct {
	NfInstanceID       string `json:"nfInstanceId"`
	Success            bool   `json:"success"`
	TimeStamp          string `json:"timeStamp"`
	AuthType           string `json:"authType"`
	ServingNetworkName string `json:"servingNetworkName"`
}

type AuthEventResponse struct {
	AuthEventID string `json:"authEventId"`
}

type Amf3GppAccessRegistration struct {
	AmfInstanceID    string `json:"amfInstanceId"`
	DeregCallbackURI string `json:"deregCallbackUri"`
	GUAMI            GUAMI  `json:"guami"`
	RATType          string `json:"ratType"`
	InitialRegInd    bool   `json:"initialRegistrationInd"`
	PEI              string `json:"pei,omitempty"`
}

type ProblemDetails struct {
	Status int    `json:"status"`
	Cause  string `json:"cause"`
	Detail string `json:"detail,omitempty"`
}

type AuthContext struct {
	ID                 string    `json:"id"`
	SUPI               string    `json:"supi"`
	SUCI               string    `json:"suci"`
	ServingNetworkName string    `json:"servingNetworkName"`
	AuthType           AuthType  `json:"authType"`
	RAND               []byte    `json:"-"`
	AUTN               []byte    `json:"-"`
	XresStar           []byte    `json:"-"`
	Kausf              []byte    `json:"-"`
	Kseaf              []byte    `json:"-"`
	HxresStar          []byte    `json:"-"`
	AllowedNSSAI       []SNSSAI  `json:"allowedNssai,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
	Confirmed          bool      `json:"confirmed"`
	IMEI               string    `json:"imei,omitempty"`
	SoftwareVersion    string    `json:"softwareVersion,omitempty"`
}

func (p ProblemDetails) Error() string {
	if p.Detail != "" {
		return p.Detail
	}
	return p.Cause
}
