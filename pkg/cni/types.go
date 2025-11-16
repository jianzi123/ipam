package cni

import (
	"encoding/json"
	"fmt"
)

// CNI specification version
const (
	CNIVersion040 = "0.4.0"
	CNIVersion100 = "1.0.0"
)

// NetConf represents the CNI network configuration
type NetConf struct {
	CNIVersion string `json:"cniVersion"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	IPAM       *IPAM  `json:"ipam,omitempty"`
}

// IPAM represents the IPAM configuration
type IPAM struct {
	Type          string   `json:"type"`
	DaemonSocket  string   `json:"daemonSocket,omitempty"`
	ClusterCIDR   string   `json:"clusterCIDR,omitempty"`
	NodeBlockSize int      `json:"nodeBlockSize,omitempty"`
	Routes        []*Route `json:"routes,omitempty"`
}

// Route represents a network route
type Route struct {
	Dst string `json:"dst"`
	GW  string `json:"gw,omitempty"`
}

// Result represents the CNI result (0.4.0 format)
type Result struct {
	CNIVersion string      `json:"cniVersion"`
	IPs        []*IPConfig `json:"ips,omitempty"`
	Routes     []*Route    `json:"routes,omitempty"`
	DNS        DNS         `json:"dns,omitempty"`
}

// IPConfig represents an IP configuration
type IPConfig struct {
	Address string `json:"address"` // IP with prefix, e.g., "10.244.1.5/24"
	Gateway string `json:"gateway,omitempty"`
}

// DNS represents DNS configuration
type DNS struct {
	Nameservers []string `json:"nameservers,omitempty"`
	Domain      string   `json:"domain,omitempty"`
	Search      []string `json:"search,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// Error represents a CNI error
type Error struct {
	CNIVersion string `json:"cniVersion"`
	Code       uint   `json:"code"`
	Msg        string `json:"msg"`
	Details    string `json:"details,omitempty"`
}

// Error codes
const (
	ErrCodeIncompatibleCNIVersion uint = 1
	ErrCodeUnsupportedField       uint = 2
	ErrCodeUnknownContainer       uint = 3
	ErrCodeInvalidEnvironmentVar  uint = 4
	ErrCodeIOFailure              uint = 5
	ErrCodeDecodingFailure        uint = 6
	ErrCodeInvalidNetworkConfig   uint = 7
	ErrCodeTryAgainLater          uint = 11
	ErrCodeInternal               uint = 999
)

// NewError creates a new CNI error
func NewError(code uint, msg string, details string) *Error {
	return &Error{
		CNIVersion: CNIVersion040,
		Code:       code,
		Msg:        msg,
		Details:    details,
	}
}

// Error implements error interface
func (e *Error) Error() string {
	return fmt.Sprintf("CNI error (code %d): %s - %s", e.Code, e.Msg, e.Details)
}

// Print prints the error in JSON format (CNI spec)
func (e *Error) Print() {
	data, _ := json.Marshal(e)
	fmt.Println(string(data))
}

// PrintResult prints the result in JSON format (CNI spec)
func (r *Result) Print() error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// VersionResult represents the VERSION command result
type VersionResult struct {
	CNIVersion        string   `json:"cniVersion"`
	SupportedVersions []string `json:"supportedVersions"`
}

// NewVersionResult creates a new version result
func NewVersionResult() *VersionResult {
	return &VersionResult{
		CNIVersion:        CNIVersion040,
		SupportedVersions: []string{CNIVersion040, CNIVersion100},
	}
}

// Print prints the version result
func (v *VersionResult) Print() error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
