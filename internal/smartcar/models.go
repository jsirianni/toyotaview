package smartcar

import (
	"encoding/json"
	"strings"
	"time"
)

type SignalState string

const (
	SignalStateOK          SignalState = "ok"
	SignalStateUnsupported SignalState = "unsupported"
	SignalStateUnavailable SignalState = "unavailable"
	SignalStateError       SignalState = "error"
)

type Vehicle struct {
	ID             string
	Make           string
	Model          string
	Year           int
	PowertrainType string
	Mode           string
}

type Signal struct {
	Code         string
	Name         string
	Group        string
	Status       string
	Unit         string
	Value        any
	Body         json.RawMessage
	RetrievedAt  time.Time
	OEMUpdatedAt time.Time
	IngestedAt   time.Time
}

type SignalSnapshot struct {
	Signal Signal
	Err    error
	State  SignalState
}

type VehicleSnapshot struct {
	Vehicle     Vehicle
	Signals     map[string]SignalSnapshot
	RefreshedAt time.Time
	Partial     bool
	Err         error
}

type Connection struct {
	ID          string
	VehicleID   string
	UserID      string
	Permissions []string
	Vehicle     Vehicle
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastUsedAt  time.Time
}

type connectionsResponse struct {
	Data  []connectionData   `json:"data"`
	Links connectionLinks    `json:"links"`
	Meta  connectionMetaData `json:"meta"`
}

type connectionData struct {
	ID            string                `json:"id"`
	Type          string                `json:"type"`
	Attributes    connectionAttributes  `json:"attributes"`
	Relationships connectionRelationSet `json:"relationships"`
	Meta          connectionTimestamps  `json:"meta"`
}

type connectionAttributes struct {
	Permissions []string          `json:"permissions"`
	Vehicle     vehicleAttributes `json:"vehicle"`
}

type connectionRelationSet struct {
	Vehicle resourceRelationship `json:"vehicle"`
	User    resourceRelationship `json:"user"`
}

type resourceRelationship struct {
	Data resourceID `json:"data"`
}

type resourceID struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type connectionTimestamps struct {
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
	LastUsedAt *string `json:"lastUsedAt"`
}

type connectionLinks struct {
	Next string `json:"next"`
}

type connectionMetaData struct {
	PageNumber int `json:"pageNumber"`
	PageSize   int `json:"pageSize"`
	TotalCount int `json:"totalCount"`
}

type vehicleResponse struct {
	Data vehicleData `json:"data"`
}

type vehicleData struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Attributes vehicleAttributes `json:"attributes"`
}

type vehicleAttributes struct {
	Make           string `json:"make"`
	Model          string `json:"model"`
	Year           int    `json:"year"`
	PowertrainType string `json:"powertrainType"`
	Mode           string `json:"mode"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type signalEnvelope struct {
	Data json.RawMessage `json:"data"`
}

func signalNameFromCode(code string) string {
	parts := strings.Split(code, "-")
	for i, part := range parts {
		parts[i] = titleWord(part)
	}

	return strings.Join(parts, " ")
}

func signalGroupFromCode(code string) string {
	parts := strings.SplitN(code, "-", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "Signal"
	}

	return titleWord(parts[0])
}

func titleWord(value string) string {
	if value == "" {
		return value
	}

	return strings.ToUpper(value[:1]) + value[1:]
}
