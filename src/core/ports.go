package core

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// PortSpan is an inclusive port range. * is 1-65535.
type PortSpan struct {
	Start uint16
	End   uint16
}

func (s PortSpan) Len() int {
	return int(s.End) - int(s.Start) + 1
}

// Canon is "80", "1000-1002", or "*" for every port.
func (s PortSpan) Canon() string {
	if s.Start == s.End {
		return strconv.Itoa(int(s.Start))
	}
	if s.Start == 1 && s.End == 65535 {
		return "*"
	}
	return fmt.Sprintf("%d-%d", s.Start, s.End)
}

// ParsePortSpan accepts "80", "1000-1002", and "*".
func ParsePortSpan(s string) (PortSpan, error) {
	s = strings.TrimSpace(s)
	if s == "*" {
		return PortSpan{Start: 1, End: 65535}, nil
	}
	if a, b, ok := strings.Cut(s, "-"); ok {
		start, err := parsePort(a)
		if err != nil {
			return PortSpan{}, fmt.Errorf("invalid port range %q", s)
		}
		end, err := parsePort(b)
		if err != nil || start > end {
			return PortSpan{}, fmt.Errorf("invalid port range %q", s)
		}
		return PortSpan{Start: start, End: end}, nil
	}
	p, err := parsePort(s)
	if err != nil {
		return PortSpan{}, fmt.Errorf("invalid port %q", s)
	}
	return PortSpan{Start: p, End: p}, nil
}

func parsePort(s string) (uint16, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("port must be 1-65535")
	}
	return uint16(n), nil
}

// ParseListenExpr accepts "80", "1000-1002", "*", "1.2.3.4:80", "1.2.3.4:1000-1002", "1.2.3.4:*".
func ParseListenExpr(s string) (host string, span PortSpan, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", PortSpan{}, fmt.Errorf("listen required")
	}
	host = "0.0.0.0"
	portPart := s
	if i := strings.LastIndex(s, ":"); i >= 0 {
		host = strings.TrimSpace(s[:i])
		portPart = s[i+1:]
		if host == "" {
			host = "0.0.0.0"
		}
		host = strings.Trim(host, "[]")
		if net.ParseIP(host) == nil {
			return "", PortSpan{}, fmt.Errorf("listen must be port, ip:port, a range, or *")
		}
	}
	span, err = ParsePortSpan(portPart)
	if err != nil {
		return "", PortSpan{}, fmt.Errorf("listen must be port, ip:port, a range, or *")
	}
	return host, span, nil
}

// ResolveDest maps a listen span onto destination ports.
// destExpr "*" keeps each listen port. A single dest port is the start of a
// run with the same length as listen. An explicit range must be that length.
// An empty destExpr uses destPort as a single port (or the start of a run).
func ResolveDest(listen PortSpan, destExpr string, destPort uint16) (PortSpan, error) {
	if listen.Len() < 1 {
		return PortSpan{}, fmt.Errorf("listen range required")
	}
	destExpr = strings.TrimSpace(destExpr)
	if destExpr == "*" {
		return listen, nil
	}
	var dest PortSpan
	var err error
	if destExpr == "" {
		if destPort == 0 {
			return PortSpan{}, fmt.Errorf("dest port required")
		}
		dest = PortSpan{Start: destPort, End: destPort}
	} else {
		dest, err = ParsePortSpan(destExpr)
		if err != nil {
			return PortSpan{}, fmt.Errorf("dest must be port, a range, or *")
		}
	}
	if dest.Len() == 1 && listen.Len() > 1 {
		end := int(dest.Start) + listen.Len() - 1
		if end > 65535 {
			return PortSpan{}, fmt.Errorf("dest range exceeds 65535")
		}
		return PortSpan{Start: dest.Start, End: uint16(end)}, nil
	}
	if dest.Len() != listen.Len() {
		return PortSpan{}, fmt.Errorf("dest range length must match listen")
	}
	return dest, nil
}
