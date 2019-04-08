package mailck

import (
	"context"
	"fmt"
	"github.com/siebenmann/smtpd"
	"github.com/stretchr/testify/assert"
	"net"
	"testing"
	"time"
)

func assertResultState(t *testing.T, result Result, expected ResultState) {
	assert.Equal(t, result.IsValid(), expected == ValidState)
	assert.Equal(t, result.IsInvalid(), expected == InvalidState)
	assert.Equal(t, result.IsError(), expected == ErrorState)
}

func TestCheckSyntax(t *testing.T) {
	tests := []struct {
		mail  string
		valid bool
	}{
		{"", false},
		{"xxx", false},
		{"s.mancketarent.de", false},
		{"s.mancke@tarentde", false},
		{"s.mancke@tarent@sdc.de", false},
		{"s.mancke@tarent.de", true},
		{"s.Mancke+yzz42@tarent.de", true},
	}

	for _, test := range tests {
		t.Run(test.mail, func(t *testing.T) {
			result := CheckSyntax(test.mail)
			assert.Equal(t, test.valid, result)
		})
	}
}

func TestCheckWithoutConnect(t *testing.T) {
	tests := []struct {
		mail          string
		result        Result
		err           error
		expectedState ResultState
	}{
		{"xxx", InvalidSyntax, nil, InvalidState},
		{"s.mancke@sdcsdcsdcsdctarent.de", ValidWithoutTestConnect, nil, ValidState},
		{"foo@example.com", Disposable, nil, InvalidState},
		{"foo@mailinator.com", Disposable, nil, InvalidState},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("regular %v", test.mail), func(t *testing.T) {
			start := time.Now()
			result, err := CheckWithoutConnect(test.mail)
			assert.Equal(t, test.result, result)
			assert.Equal(t, test.err, err)
			assertResultState(t, result, test.expectedState)
			fmt.Printf("check for %30v: %-15v => %-10v (%v)\n", test.mail, time.Since(start), test.result.Result, test.result.ResultDetail)
		})
	}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		mail          string
		result        Result
		err           error
		expectedState ResultState
	}{
		{"xxx", InvalidSyntax, nil, InvalidState},
		{"s.mancke@sdcsdcsdcsdctarent.de", InvalidDomain, nil, InvalidState},
		{"foo@example.com", InvalidDomain, nil, InvalidState},
		{"foo@mailinator.com", Disposable, nil, InvalidState},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("regular %v", test.mail), func(t *testing.T) {
			start := time.Now()
			result, err := Check("noreply@mancke.net", test.mail)
			assert.Equal(t, test.result, result)
			assert.Equal(t, test.err, err)
			assertResultState(t, result, test.expectedState)
			fmt.Printf("check for %30v: %-15v => %-10v (%v)\n", test.mail, time.Since(start), test.result.Result, test.result.ResultDetail)
		})
		t.Run(fmt.Sprintf("context %v", test.mail), func(t *testing.T) {
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			result, err := CheckWithContext(ctx, "noreply@mancke.net", test.mail)
			assert.Equal(t, test.result, result)
			assert.Equal(t, test.err, err)
			assertResultState(t, result, test.expectedState)
			assert.WithinDuration(t, time.Now(), start, 160*time.Millisecond)
			fmt.Printf("check for %30v: %-15v => %-10v (%v)\n", test.mail, time.Since(start), test.result.Result, test.result.ResultDetail)
			cancel()
		})
	}
}

func Test_checkMailbox(t *testing.T) {
	tests := []struct {
		stopAt        smtpd.Command
		result        Result
		expectError   bool
		expectedState ResultState
	}{
		{smtpd.QUIT, Valid, false, ValidState},
		{smtpd.RCPTTO, MailboxUnavailable, false, InvalidState},
		{smtpd.MAILFROM, MailserverError, true, ErrorState},
		{smtpd.HELO, MailserverError, true, ErrorState},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("stop at: %v", test.stopAt), func(t *testing.T) {
			dummyServer := NewDummySMTPServer("localhost:2525", test.stopAt, false, 0)
			defer dummyServer.Close()
			result, err := checkMailbox(noContext, "noreply@mancke.net", "foo@bar.de", []*net.MX{{Host: "localhost"}}, 2525)
			assert.Equal(t, test.result, result)
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assertResultState(t, result, test.expectedState)
		})
	}
}

func Test_checkMailbox_MailserverCloesAfterConnect(t *testing.T) {
	dummyServer := NewDummySMTPServer("localhost:2525", smtpd.NOOP, true, 0)
	defer dummyServer.Close()
	result, err := checkMailbox(noContext, "noreply@mancke.net", "foo@bar.de", []*net.MX{{Host: "localhost"}}, 2525)
	assert.Equal(t, MailserverError, result)
	assert.Error(t, err)
	assertResultState(t, result, ErrorState)
}

func Test_checkMailbox_NetworkError(t *testing.T) {
	result, err := checkMailbox(noContext, "noreply@mancke.net", "foo@bar.de", []*net.MX{{Host: "localhost"}}, 6666)
	assert.Equal(t, NetworkError, result)
	assert.Error(t, err)
	assertResultState(t, result, ErrorState)
}

func Test_checkMailboxContext(t *testing.T) {
	deltas := []struct {
		delayTime      time.Duration
		contextTime    time.Duration
		expectedResult Result
	}{
		{0, 0, TimeoutError},
		{0, time.Second, Valid},
		{time.Millisecond * 1500, 200 * time.Millisecond, TimeoutError},
	}
	for _, d := range deltas {
		t.Run(fmt.Sprintf("context time %v delay %v expected %v", d.contextTime, d.delayTime, d.expectedResult.Result), func(t *testing.T) {
			dummyServer := NewDummySMTPServer("localhost:2528", smtpd.QUIT, false, d.delayTime)
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), d.contextTime)
			result, err := checkMailbox(ctx, "noreply@mancke.net", "foo@bar.de", []*net.MX{{Host: "127.0.0.1"}}, 2528)
			if d.expectedResult == Valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
			assert.Equal(t, d.expectedResult, result)
			// confirm that we completed within requested time
			// add 10ms of wiggle room
			assert.WithinDuration(t, time.Now(), start, d.contextTime+10*time.Millisecond)
			dummyServer.Close()
			cancel()
		})
	}
}

type DummySMTPServer struct {
	listener          net.Listener
	running           bool
	rejectAt          smtpd.Command
	closeAfterConnect bool
	delay             time.Duration
}

func NewDummySMTPServer(listen string, rejectAt smtpd.Command, closeAfterConnect bool, delay time.Duration) *DummySMTPServer {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		panic(err)
	}
	time.Sleep(10 * time.Millisecond)
	smtpserver := &DummySMTPServer{
		listener:          ln,
		running:           true,
		rejectAt:          rejectAt,
		closeAfterConnect: closeAfterConnect,
		delay:             delay,
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if smtpserver.closeAfterConnect {
				conn.Close()
			} else {
				go smtpserver.handleClient(conn)
			}
		}
	}()
	time.Sleep(10 * time.Millisecond)
	return smtpserver
}

func (smtpserver *DummySMTPServer) Close() {
	smtpserver.listener.Close()
	smtpserver.running = false
	time.Sleep(10 * time.Millisecond)
}

func (smtpserver *DummySMTPServer) handleClient(conn net.Conn) {
	cfg := smtpd.Config{
		LocalName: "testserver",
		SftName:   "testserver",
	}
	c := smtpd.NewConn(conn, cfg, nil)
	for smtpserver.running {
		event := c.Next()
		time.Sleep(smtpserver.delay)
		if event.Cmd == smtpserver.rejectAt ||
			(smtpserver.rejectAt == smtpd.HELO && event.Cmd == smtpd.EHLO) {
			c.Reject()
		} else {
			c.Accept()
		}
		if event.What == smtpd.DONE {
			return
		}
	}
}
