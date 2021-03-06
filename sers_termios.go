// +build darwin linux

package sers

// Copyright 2012 Michael Meier. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

/*#include <stddef.h>
#include <stdlib.h>
#include <termios.h>
#include <sys/ioctl.h>


 extern int ioctl1(int i, unsigned int r, void *d);
*/
import "C"

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	// using C.IOSSIOSPEED yields 0x80085402 
	// which does not work. don't ask me why
	// this define is wrong in cgo.
	IOSSIOSPEED = 0x80045402
)

type baseport struct {
	f *os.File
}

func TakeOver(f *os.File) (SerialPort, error) {
	if f == nil {
		return nil, &ParameterError{"f", "needs to be non-nil"}
	}
	bp := &baseport{f}

	tio, err := bp.getattr()
	if err != nil {
		return nil, &Error{"putting fd in non-canonical mode", err}
	}

	C.cfmakeraw(tio)

	err = bp.setattr(tio)
	if err != nil {
		return nil, &Error{"putting fd in non-canonical mode", err}
	}

	return bp, nil
}

func (bp *baseport) Read(b []byte) (int, error) {
	return bp.f.Read(b)
}

func (b *baseport) Close() error {
	return b.f.Close()
}

func (bp *baseport) Write(b []byte) (int, error) {
	return bp.f.Write(b)
}

func (bp *baseport) getattr() (*C.struct_termios, error) {
	var tio C.struct_termios
	res, err := C.tcgetattr(C.int(bp.f.Fd()), (*C.struct_termios)(unsafe.Pointer(&tio)))
	if res != 0 || err != nil {
		return nil, err
	}

	return &tio, nil
}

func (bp *baseport) setattr(tio *C.struct_termios) error {
	res, err := C.tcsetattr(C.int(bp.f.Fd()), C.TCSANOW, (*C.struct_termios)(unsafe.Pointer(tio)))
	if res != 0 || err != nil {
		return err
	}

	return nil
}

func (bp *baseport) SetMode(baudrate, databits, parity, stopbits, handshake int) error {
	if baudrate <= 0 {
		return &ParameterError{"baudrate", "has to be > 0"}
	}

	var datamask uint
	switch databits {
	case 5:
		datamask = C.CS5
	case 6:
		datamask = C.CS6
	case 7:
		datamask = C.CS7
	case 8:
		datamask = C.CS8
	default:
		return &ParameterError{"databits", "has to be 5, 6, 7 or 8"}
	}

	if stopbits != 1 && stopbits != 2 {
		return &ParameterError{"stopbits", "has to be 1 or 2"}
	}
	var stopmask uint
	if stopbits == 2 {
		stopmask = C.CSTOPB
	}

	var parmask uint
	switch parity {
	case N:
		parmask = 0
	case E:
		parmask = C.PARENB
	case O:
		parmask = C.PARENB | C.PARODD
	default:
		return &ParameterError{"parity", "has to be N, E or O"}
	}

	var flowmask uint
	switch handshake {
	case NO_HANDSHAKE:
		flowmask = 0
	case RTSCTS_HANDSHAKE:
		flowmask = C.CRTSCTS
	default:
		return &ParameterError{"handshake", "has to be NO_HANDSHAKE or RTSCTS_HANDSHAKE"}
	}

	tio, err := bp.getattr()
	if err != nil {
		return err
	}

	tio.c_cflag &^= C.CSIZE
	tio.c_cflag |= C.tcflag_t(datamask)

	tio.c_cflag &^= C.PARENB | C.PARODD
	tio.c_cflag |= C.tcflag_t(parmask)

	tio.c_cflag &^= C.CSTOPB
	tio.c_cflag |= C.tcflag_t(stopmask)

	tio.c_cflag &^= C.CRTSCTS
	tio.c_cflag |= C.tcflag_t(flowmask)

	if err := bp.setattr(tio); err != nil {
		return err
	}

	if err := bp.SetBaudRate(baudrate); err != nil {
		return err
	}

	return nil
}

func (bp *baseport) SetReadParams(minread int, timeout float64) error {
	inttimeout := int(timeout * 10)
	if inttimeout < 0 {
		return &ParameterError{"timeout", "needs to be 0 or higher"}
	}
	// if a timeout is desired but too small for the termios timeout
	// granularity, set the minimum timeout
	if timeout > 0 && inttimeout == 0 {
		inttimeout = 1
	}

	tio, err := bp.getattr()
	if err != nil {
		return err
	}

	tio.c_cc[C.VMIN] = C.cc_t(minread)
	tio.c_cc[C.VTIME] = C.cc_t(inttimeout)

	//fmt.Printf("baud rates from termios: %d, %d\n", tio.c_ispeed, tio.c_ospeed)

	err = bp.setattr(tio)
	if err != nil {
		return err
	}

	return nil
}

func Open(fn string) (SerialPort, error) {
	// the order of system calls is taken from Apple's SerialPortSample
	// open the TTY device read/write, nonblocking, i.e. not waiting
	// for the CARRIER signal and without the TTY controlling the process
	f, err := os.OpenFile(fn, syscall.O_RDWR|
		syscall.O_NONBLOCK|
		syscall.O_NOCTTY, 0666)
	if err != nil {
		return nil, err
	}

	s, err := TakeOver(f)
	if err != nil {
		return nil, err
	}

	// clear non-blocking mode
	err = s.(*baseport).ClearNonBlocking()
	if err != nil {
		f.Close()
		return nil, &Error{"putting fd into non-blocking mode", err}
	}

	return s, nil
}
