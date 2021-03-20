/*
 * @Author: ph4ntom
 * @Date: 2021-03-09 14:02:57
 * @LastEditors: ph4ntom
 * @LastEditTime: 2021-03-20 15:32:52
 */
package protocol

import (
	"Stowaway/crypto"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
)

type TCPMessage struct {
	// Essential component to apply a Message
	ID           string
	Conn         net.Conn
	CryptoSecret []byte
	// flag to mark if the packet needed to be proxy
	IsPass bool
	// Prepared buffer
	HeaderBuffer []byte
	DataBuffer   []byte
}

/**
 * @description: Tcp raw meesage do not need special header
 * @param {*}
 * @return {*}
 */
func (message *TCPMessage) ConstructHeader() {}

/**
 * @description: Construct our own raw tcp data
 * @param {*}
 * @return {*}
 */
func (message *TCPMessage) ConstructData(header Header, mess interface{}) {
	var headerBuffer bytes.Buffer
	// First, construct own header
	messageTypeBuf := make([]byte, 2)
	routeLenBuf := make([]byte, 4)

	binary.BigEndian.PutUint16(messageTypeBuf, header.MessageType)
	binary.BigEndian.PutUint32(routeLenBuf, header.RouteLen)

	// Write header into buffer(except for dataLen)
	headerBuffer.Write([]byte(header.Sender))
	headerBuffer.Write([]byte(header.Accepter))
	headerBuffer.Write(messageTypeBuf)
	headerBuffer.Write(routeLenBuf)
	headerBuffer.Write([]byte(header.Route))

	// Check if message's data is needed to encrypt
	if message.IsPass && message.DataBuffer != nil {
		message.IsPass = false
	} else {
		switch header.MessageType {
		case HI:
			mmess := mess.(HIMess)
			greetingLenBuf := make([]byte, 2)
			binary.BigEndian.PutUint16(greetingLenBuf, mmess.GreetingLen)

			greetingBuf := []byte(mmess.Greeting)

			isAdminBuf := make([]byte, 2)
			binary.BigEndian.PutUint16(isAdminBuf, mmess.IsAdmin)
			// Collect all spilted data, try encrypt them
			// use message.DataBuffer directly to save memory
			message.DataBuffer = append(message.DataBuffer, greetingLenBuf...)
			message.DataBuffer = append(message.DataBuffer, greetingBuf...)
			message.DataBuffer = append(message.DataBuffer, isAdminBuf...)
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case UUID:
			mmess := mess.(UUIDMess)
			uuidLenBuf := make([]byte, 2)
			binary.BigEndian.PutUint16(uuidLenBuf, mmess.UUIDLen)

			uuidBuf := []byte(mmess.UUID)

			message.DataBuffer = append(message.DataBuffer, uuidLenBuf...)
			message.DataBuffer = append(message.DataBuffer, uuidBuf...)
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case UUIDRET:
			mmess := mess.(UUIDRetMess)
			OKBuf := make([]byte, 2)
			binary.BigEndian.PutUint16(OKBuf, mmess.OK)

			message.DataBuffer = OKBuf
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case MYINFO:
			mmess := mess.(MyInfo)
			usernameLenBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(usernameLenBuf, mmess.UsernameLen)

			usernameBuf := []byte(mmess.Username)

			hostnameLenBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(hostnameLenBuf, mmess.HostnameLen)

			hostnameBuf := []byte(mmess.Hostname)

			message.DataBuffer = append(message.DataBuffer, usernameLenBuf...)
			message.DataBuffer = append(message.DataBuffer, usernameBuf...)
			message.DataBuffer = append(message.DataBuffer, hostnameLenBuf...)
			message.DataBuffer = append(message.DataBuffer, hostnameBuf...)
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case MYMEMO:
			mmess := mess.(MyMemo)
			memoLenBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(memoLenBuf, mmess.MemoLen)

			memoBuf := []byte(mmess.Memo)

			message.DataBuffer = append(message.DataBuffer, memoLenBuf...)
			message.DataBuffer = append(message.DataBuffer, memoBuf...)
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case SHELLREQ:
			mmess := mess.(ShellReq)
			startBuf := make([]byte, 2)
			binary.BigEndian.PutUint16(startBuf, mmess.Start)

			message.DataBuffer = startBuf
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case SHELLRES:
			mmess := mess.(ShellRes)
			OKBuf := make([]byte, 2)
			binary.BigEndian.PutUint16(OKBuf, mmess.OK)

			message.DataBuffer = OKBuf
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case SHELLCOMMAND:
			mmess := mess.(ShellCommand)
			commandLenBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(commandLenBuf, mmess.CommandLen)

			commandBuf := []byte(mmess.Command)

			message.DataBuffer = append(message.DataBuffer, commandLenBuf...)
			message.DataBuffer = append(message.DataBuffer, commandBuf...)
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case SHELLRESULT:
			mmess := mess.(ShellResult)

			resultLenBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(resultLenBuf, mmess.ResultLen)

			resultBuf := []byte(mmess.Result)

			message.DataBuffer = append(message.DataBuffer, resultLenBuf...)
			message.DataBuffer = append(message.DataBuffer, resultBuf...)
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case LISTENREQ:
			mmess := mess.(ListenReq)
			addrLenBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(addrLenBuf, mmess.AddrLen)

			addrBuf := []byte(mmess.Addr)

			message.DataBuffer = append(message.DataBuffer, addrLenBuf...)
			message.DataBuffer = append(message.DataBuffer, addrBuf...)
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case LISTENRES:
			mmess := mess.(ListenRes)
			OKBuf := make([]byte, 2)
			binary.BigEndian.PutUint16(OKBuf, mmess.OK)

			message.DataBuffer = OKBuf
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case SSHREQ:
			mmess := mess.(SSHReq)
			methodBuf := make([]byte, 2)
			binary.BigEndian.PutUint16(methodBuf, mmess.Method)

			addrLenBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(addrLenBuf, mmess.AddrLen)

			addrBuf := []byte(mmess.Addr)

			usernameLenBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(usernameLenBuf, mmess.UsernameLen)

			usernameBuf := []byte(mmess.Username)

			passwordLenBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(passwordLenBuf, mmess.PasswordLen)

			passwordBuf := []byte(mmess.Password)

			certificateLenBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(certificateLenBuf, mmess.CertificateLen)

			certificateBuf := mmess.Certificate

			message.DataBuffer = append(message.DataBuffer, methodBuf...)
			message.DataBuffer = append(message.DataBuffer, addrLenBuf...)
			message.DataBuffer = append(message.DataBuffer, addrBuf...)
			message.DataBuffer = append(message.DataBuffer, usernameLenBuf...)
			message.DataBuffer = append(message.DataBuffer, usernameBuf...)
			message.DataBuffer = append(message.DataBuffer, passwordLenBuf...)
			message.DataBuffer = append(message.DataBuffer, passwordBuf...)
			message.DataBuffer = append(message.DataBuffer, certificateLenBuf...)
			message.DataBuffer = append(message.DataBuffer, certificateBuf...)
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case SSHRES:
			mmess := mess.(SSHRes)
			OKBuf := make([]byte, 2)
			binary.BigEndian.PutUint16(OKBuf, mmess.OK)

			message.DataBuffer = OKBuf
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case SSHCOMMAND:
			mmess := mess.(SSHCommand)

			commandLenBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(commandLenBuf, mmess.CommandLen)

			commandBuf := []byte(mmess.Command)

			message.DataBuffer = append(message.DataBuffer, commandLenBuf...)
			message.DataBuffer = append(message.DataBuffer, commandBuf...)
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		case SSHRESULT:
			mmess := mess.(SSHResult)

			resultLenBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(resultLenBuf, mmess.ResultLen)

			resultBuf := []byte(mmess.Result)

			message.DataBuffer = append(message.DataBuffer, resultLenBuf...)
			message.DataBuffer = append(message.DataBuffer, resultBuf...)
			message.DataBuffer = crypto.AESEncrypt(message.DataBuffer, message.CryptoSecret)
		default:
		}
	}
	// Calculate the whole data's length
	dataLenBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(dataLenBuf, uint64(len(message.DataBuffer)))
	headerBuffer.Write(dataLenBuf)

	message.HeaderBuffer = headerBuffer.Bytes()
}

/**
 * @description: Tcp raw meesage do not need special suffix
 * @param {*}
 * @return {*}
 */
func (message *TCPMessage) ConstructSuffix() {}

/**
 * @description: Tcp raw meesage do not need to deconstruct special header
 * @param {*}
 * @return {*}
 */
func (message *TCPMessage) DeconstructHeader() {}

/**
 * @description: Deconstruct our own raw tcp data
 * @param {*}
 * @return {*}
 */
func (message *TCPMessage) DeconstructData() (Header, interface{}, error) {
	var (
		header         = Header{}
		senderBuf      = make([]byte, 10)
		accepterBuf    = make([]byte, 10)
		messageTypeBuf = make([]byte, 2)
		routeLenBuf    = make([]byte, 4)
		dataLenBuf     = make([]byte, 8)
	)

	var err error

	_, err = io.ReadFull(message.Conn, senderBuf)
	if err != nil {
		return header, nil, err
	}
	header.Sender = string(senderBuf)

	_, err = io.ReadFull(message.Conn, accepterBuf)
	if err != nil {
		return header, nil, err
	}
	header.Accepter = string(accepterBuf)

	_, err = io.ReadFull(message.Conn, messageTypeBuf)
	if err != nil {
		return header, nil, err
	}
	header.MessageType = binary.BigEndian.Uint16(messageTypeBuf)

	_, err = io.ReadFull(message.Conn, routeLenBuf)
	if err != nil {
		return header, nil, err
	}
	header.RouteLen = binary.BigEndian.Uint32(routeLenBuf)

	routeBuf := make([]byte, header.RouteLen)
	_, err = io.ReadFull(message.Conn, routeBuf)
	if err != nil {
		return header, nil, err
	}
	header.Route = string(routeBuf)

	_, err = io.ReadFull(message.Conn, dataLenBuf)
	if err != nil {
		return header, nil, err
	}
	header.DataLen = binary.BigEndian.Uint64(dataLenBuf)

	dataBuf := make([]byte, header.DataLen)
	_, err = io.ReadFull(message.Conn, dataBuf)
	if err != nil {
		return header, nil, err
	}

	if header.Accepter == TEMP_UUID || message.ID == ADMIN_UUID || message.ID == header.Accepter {
		dataBuf = crypto.AESDecrypt(dataBuf[:], message.CryptoSecret) // use dataBuf directly to save the memory
	} else {
		message.IsPass = true
		message.DataBuffer = dataBuf
		return header, nil, nil
	}

	switch header.MessageType {
	case HI:
		mmess := new(HIMess)
		mmess.GreetingLen = binary.BigEndian.Uint16(dataBuf[:2])
		mmess.Greeting = string(dataBuf[2 : 2+mmess.GreetingLen])
		mmess.IsAdmin = binary.BigEndian.Uint16(dataBuf[2+mmess.GreetingLen : header.DataLen])
		return header, mmess, nil
	case UUID:
		mmess := new(UUIDMess)
		mmess.UUIDLen = binary.BigEndian.Uint16(dataBuf[:2])
		mmess.UUID = string(dataBuf[2 : 2+mmess.UUIDLen])
		return header, mmess, nil
	case UUIDRET:
		mmess := new(UUIDRetMess)
		mmess.OK = binary.BigEndian.Uint16(dataBuf[:2])
		return header, mmess, nil
	case MYINFO:
		mmess := new(MyInfo)
		mmess.UsernameLen = binary.BigEndian.Uint64(dataBuf[:8])
		mmess.Username = string(dataBuf[8 : 8+mmess.UsernameLen])
		mmess.HostnameLen = binary.BigEndian.Uint64(dataBuf[8+mmess.UsernameLen : 16+mmess.UsernameLen])
		mmess.Hostname = string(dataBuf[16+mmess.UsernameLen : 16+mmess.UsernameLen+mmess.HostnameLen])
		return header, mmess, nil
	case MYMEMO:
		mmess := new(MyMemo)
		mmess.MemoLen = binary.BigEndian.Uint64(dataBuf[:8])
		mmess.Memo = string(dataBuf[8 : 8+mmess.MemoLen])
		return header, mmess, nil
	case SHELLREQ:
		mmess := new(ShellReq)
		mmess.Start = binary.BigEndian.Uint16(dataBuf[:2])
		return header, mmess, nil
	case SHELLRES:
		mmess := new(ShellRes)
		mmess.OK = binary.BigEndian.Uint16(dataBuf[:2])
		return header, mmess, nil
	case SHELLCOMMAND:
		mmess := new(ShellCommand)
		mmess.CommandLen = binary.BigEndian.Uint64(dataBuf[:8])
		mmess.Command = string(dataBuf[8 : 8+mmess.CommandLen])
		return header, mmess, nil
	case SHELLRESULT:
		mmess := new(ShellResult)
		mmess.ResultLen = binary.BigEndian.Uint64(dataBuf[:8])
		mmess.Result = string(dataBuf[8 : 8+mmess.ResultLen])
		return header, mmess, nil
	case LISTENREQ:
		mmess := new(ListenReq)
		mmess.AddrLen = binary.BigEndian.Uint64(dataBuf[:8])
		mmess.Addr = string(dataBuf[8 : 8+mmess.AddrLen])
		return header, mmess, nil
	case LISTENRES:
		mmess := new(ListenRes)
		mmess.OK = binary.BigEndian.Uint16(dataBuf[:2])
		return header, mmess, nil
	case SSHREQ:
		mmess := new(SSHReq)
		mmess.Method = binary.BigEndian.Uint16(dataBuf[:2])
		mmess.AddrLen = binary.BigEndian.Uint64(dataBuf[2:10])
		mmess.Addr = string(dataBuf[10 : 10+mmess.AddrLen])
		mmess.UsernameLen = binary.BigEndian.Uint64(dataBuf[10+mmess.AddrLen : 18+mmess.AddrLen])
		mmess.Username = string(dataBuf[18+mmess.AddrLen : 18+mmess.AddrLen+mmess.UsernameLen])
		mmess.PasswordLen = binary.BigEndian.Uint64(dataBuf[18+mmess.AddrLen+mmess.UsernameLen : 26+mmess.AddrLen+mmess.UsernameLen])
		mmess.Password = string(dataBuf[26+mmess.AddrLen+mmess.UsernameLen : 26+mmess.AddrLen+mmess.UsernameLen+mmess.PasswordLen])
		mmess.CertificateLen = binary.BigEndian.Uint64(dataBuf[26+mmess.AddrLen+mmess.UsernameLen+mmess.PasswordLen : 34+mmess.AddrLen+mmess.UsernameLen+mmess.PasswordLen])
		mmess.Certificate = dataBuf[34+mmess.AddrLen+mmess.UsernameLen+mmess.PasswordLen : 34+mmess.AddrLen+mmess.UsernameLen+mmess.PasswordLen+mmess.CertificateLen]
		return header, mmess, nil
	case SSHRES:
		mmess := new(SSHRes)
		mmess.OK = binary.BigEndian.Uint16(dataBuf[:2])
		return header, mmess, nil
	case SSHCOMMAND:
		mmess := new(SSHCommand)
		mmess.CommandLen = binary.BigEndian.Uint64(dataBuf[:8])
		mmess.Command = string(dataBuf[8 : 8+mmess.CommandLen])
		return header, mmess, nil
	case SSHRESULT:
		mmess := new(SSHResult)
		mmess.ResultLen = binary.BigEndian.Uint64(dataBuf[:8])
		mmess.Result = string(dataBuf[8 : 8+mmess.ResultLen])
		return header, mmess, nil
	default:
	}

	return header, nil, errors.New("Unknown error!")
}

/**
 * @description: Tcp raw meesage do not need to deconstruct special suffix
 * @param {*}
 * @return {*}
 */
func (message *TCPMessage) DeconstructSuffix() {}

/**
 * @description: Send message to peer node
 * @param {*}
 * @return {*}
 */
func (message *TCPMessage) SendMessage() {
	message.Conn.Write(message.HeaderBuffer)
	message.Conn.Write(message.DataBuffer)
	// Don't forget to set both Buffer to nil!!!
	message.HeaderBuffer = nil
	message.DataBuffer = nil
}
