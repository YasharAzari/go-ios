package forward

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/danielpaulus/go-ios/ios"
	log "github.com/sirupsen/logrus"
)

type iosproxy struct {
	tcpConn    net.Conn
	deviceConn ios.DeviceConnectionInterface
}

//Forward forwards every connection made to the hostPort to whatever service runs inside an app on the device on phonePort.
func Forward(device ios.DeviceEntry, hostPort uint16, phonePort uint16) error {

	log.Infof("Start listening on port %d forwarding to port %d on device", hostPort, phonePort)
	l, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", hostPort))

	go connectionAccept(l, device.DeviceID, phonePort)

	if err != nil {
		return err
	}

	return nil
}

func connectionAccept(l net.Listener, deviceID int, phonePort uint16) {
	var openConnectionCounter int64
	for {
		clientConn, err := l.Accept()
		if err != nil {
			log.Errorf("Error accepting new connection %v", err)
			continue
		}
		log.WithFields(log.Fields{"conn": fmt.Sprintf("%#v", clientConn)}).Info("new client connected")
		go startNewProxyConnection(clientConn, deviceID, phonePort, &openConnectionCounter)
	}
}

func startNewProxyConnection(clientConn net.Conn, deviceID int, phonePort uint16, openConnectionCounter *int64) {
	defer clientConn.Close()
	usbmuxConn, err := ios.NewUsbMuxConnectionSimple()
	if err != nil {
		log.Errorf("could not connect to usbmuxd: %+v", err)
		return
	}
	muxError := usbmuxConn.Connect(deviceID, phonePort)
	if muxError != nil {
		log.WithFields(log.Fields{"conn": fmt.Sprintf("%#v", clientConn), "err": muxError, "phonePort": phonePort}).Infof("could not connect to phone")
		return
	}
	atomic.AddInt64(openConnectionCounter, 1)
	//log.WithFields(log.Fields{"conn": fmt.Sprintf("%#v", clientConn), "phonePort": phonePort, "totalOpenConnection": openConnectionCounter}).Infof("Connected to port")
	deviceConn := usbmuxConn.ReleaseDeviceConnection()
	defer deviceConn.Close()
	var wg sync.WaitGroup
	wg.Add(2)

	buff := bytes.Buffer{}
	r := io.TeeReader(clientConn, &buff)
	var request string
	go func() {
		defer wg.Done()
		time.Sleep(500 * time.Millisecond)
		out, _ := ioutil.ReadAll(&buff)
		log.WithFields(log.Fields{"conn": fmt.Sprintf("%#v", clientConn), "phonePort": phonePort, "totalOpenConnection": openConnectionCounter, "request": string(out)}).Infof("Connected to port")
	}()
	go func() {
		//writing device response to client
		defer wg.Done()
		io.Copy(clientConn, deviceConn.Reader())
	}()
	go func() {
		//writing client request to device
		io.Copy(deviceConn.Writer(), r)
	}()
	wg.Wait()

	log.WithFields(log.Fields{"conn": fmt.Sprintf("%#v", clientConn), "phonePort": phonePort, "request": request}).Infof("Closing connection to port")
	atomic.AddInt64(openConnectionCounter, -1)
}

func (proxyConn *iosproxy) Close() {
	proxyConn.tcpConn.Close()
}
