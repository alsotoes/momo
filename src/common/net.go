package common

import (
	"errors"
	"net"
)

// DialSocket connects to the given address.
// It returns a net.Conn or an error.
func DialSocket(servAddr string) (net.Conn, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", servAddr)
	if err != nil {
		return nil, errors.New("ResolveTCPAddr failed: " + err.Error())
	}

	connection, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return nil, errors.New("Dial failed: " + err.Error())
	}

	return connection, nil
}
