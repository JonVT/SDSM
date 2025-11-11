package utils

import (
    "context"
    "net"
    "sync"
    "time"

    natlib "github.com/libp2p/go-nat"
)

// NAT is an alias to the libp2p NAT interface to avoid leaking the external package
// beyond this utility layer in most places.
type NAT = natlib.NAT

var (
    natOnce     sync.Once
    cachedNAT   NAT
    cachedNATErr error
)

// DiscoverNAT attempts to locate a NAT gateway using UPnP or NAT-PMP.
// The result is cached for the process lifetime to avoid repeated SSDP lookups.
func DiscoverNAT(ctx context.Context) (NAT, error) {
    natOnce.Do(func() {
        // Use a short timeout to avoid blocking UI/API interactions for long periods.
        c, cancel := context.WithTimeout(ctx, 5*time.Second)
        defer cancel()
        cachedNAT, cachedNATErr = natlib.DiscoverGateway(c)
    })
    return cachedNAT, cachedNATErr
}

// GetExternalIP returns the external IP address from the discovered NAT device.
func GetExternalIP(ctx context.Context) (net.IP, error) {
    n, err := DiscoverNAT(ctx)
    if err != nil || n == nil {
        return nil, err
    }
    return n.GetExternalAddress()
}

// AddOrRefreshMapping ensures a port mapping exists for the given internal port/protocol.
// Returns the external port assigned by the gateway (can differ from the internal port) and any error.
// protocol must be "udp" or "tcp".
func AddOrRefreshMapping(ctx context.Context, protocol string, internalPort int, description string, lifetime time.Duration) (int, error) {
    n, err := DiscoverNAT(ctx)
    if err != nil || n == nil {
        return 0, err
    }
    // The libp2p interface picks the external port; we request a mapping for internalPort.
    // We refresh by simply re-adding with the desired lifetime.
    c, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    return n.AddPortMapping(c, protocol, internalPort, description, lifetime)
}

// DeleteMapping removes a port mapping for the given internal port/protocol.
func DeleteMapping(ctx context.Context, protocol string, internalPort int) error {
    n, err := DiscoverNAT(ctx)
    if err != nil || n == nil {
        return err
    }
    c, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    return n.DeletePortMapping(c, protocol, internalPort)
}
