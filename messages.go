package mqtt

import (
	"bytes"
	"io"
)

type Header struct {
	DupFlag, Retain bool
	QosLevel        QosLevel
}

func (hdr *Header) Encode(w io.Writer, msgType MessageType, remainingLength int32) error {
	if !hdr.QosLevel.IsValid() {
		return badQosError
	}
	if !msgType.IsValid() {
		return badMsgTypeError
	}

	buf := new(bytes.Buffer)
	val := byte(msgType) << 4
	val |= (boolToByte(hdr.DupFlag) << 3)
	val |= byte(hdr.QosLevel) << 1
	val |= boolToByte(hdr.Retain)
	buf.WriteByte(val)
	encodeLength(remainingLength, buf)
	_, err := w.Write(buf.Bytes())
	return err
}

func (hdr *Header) Decode(r io.Reader) (msgType MessageType, remainingLength int32, err error) {
	defer func() {
		err = recoverError(err)
	}()

	var buf [1]byte

	if _, err = io.ReadFull(r, buf[:]); err != nil {
		return
	}

	byte1 := buf[0]
	msgType = MessageType(byte1 & 0xF0 >> 4)

	*hdr = Header{
		DupFlag:     byte1&0x08 > 0,
		QosLevel:    QosLevel(byte1 & 0x06 >> 1),
		Retain:      byte1&0x01 > 0,
	}

	remainingLength = decodeLength(r)

	return
}

type Message interface {
	Encode(w io.Writer) error
	Decode(r io.Reader, hdr Header, packetRemaining int32) error
}

const (
	MsgConnect = MessageType(iota + 1)
	MsgConnAck
	MsgPublish
	MsgPubAck
	MsgPubRec
	MsgPubRel
	MsgPubComp
	MsgSubscribe
	MsgSubAck
	MsgUnsubscribe
	MsgUnsubAck
	MsgPingReq
	MsgPingResp
	MsgDisconnect

	msgTypeFirstInvalid
)

type MessageType uint8

func (mt MessageType) IsValid() bool {
	return mt >= MsgConnect && mt < msgTypeFirstInvalid
}

func writeMessage(w io.Writer, hdr *Header, payloadBuf *bytes.Buffer) error {
	err := hdr.Encode(w, MsgConnect, int32(len(payloadBuf.Bytes())))
	if err != nil {
		return err
	}

	_, err = w.Write(payloadBuf.Bytes())

	return err
}

type Connect struct {
	Header
	ProtocolName               string
	ProtocolVersion            uint8
	WillRetain                 bool
	WillFlag                   bool
	CleanSession               bool
	WillQos                    QosLevel
	KeepAliveTimer             uint16
	ClientId                   string
	WillTopic, WillMessage     string
	UsernameFlag, PasswordFlag bool
	Username, Password         string
}

func (msg *Connect) Encode(w io.Writer) (err error) {
	if !msg.WillQos.IsValid() {
		return badWillQosError
	}

	buf := new(bytes.Buffer)

	flags := boolToByte(msg.UsernameFlag) << 7
	flags |= boolToByte(msg.PasswordFlag) << 6
	flags |= boolToByte(msg.WillRetain) << 5
	flags |= byte(msg.WillQos) << 3
	flags |= boolToByte(msg.WillFlag) << 2
	flags |= boolToByte(msg.CleanSession) << 1

	setString(msg.ProtocolName, buf)
	setUint8(msg.ProtocolVersion, buf)
	buf.WriteByte(flags)
	setUint16(msg.KeepAliveTimer, buf)
	setString(msg.ClientId, buf)
	if msg.WillFlag {
		setString(msg.WillTopic, buf)
		setString(msg.WillMessage, buf)
	}
	if msg.UsernameFlag {
		setString(msg.Username, buf)
	}
	if msg.PasswordFlag {
		setString(msg.Password, buf)
	}

	return writeMessage(w, &msg.Header, buf)
}

func (msg *Connect) Decode(r io.Reader, hdr Header, packetRemaining int32) (err error) {
	defer func() {
		err = recoverError(err)
	}()

	protocolName := getString(r, &packetRemaining)
	protocolVersion := getUint8(r, &packetRemaining)
	flags := getUint8(r, &packetRemaining)
	keepAliveTimer := getUint16(r, &packetRemaining)
	clientId := getString(r, &packetRemaining)

	*msg = Connect{
		ProtocolName:    protocolName,
		ProtocolVersion: protocolVersion,
		UsernameFlag:    flags&0x80 > 0,
		PasswordFlag:    flags&0x40 > 0,
		WillRetain:      flags&0x20 > 0,
		WillQos:         QosLevel(flags & 0x18 >> 3),
		WillFlag:        flags&0x04 > 0,
		CleanSession:    flags&0x02 > 0,
		KeepAliveTimer:  keepAliveTimer,
		ClientId:        clientId,
	}

	if msg.WillFlag {
		msg.WillTopic = getString(r, &packetRemaining)
		msg.WillMessage = getString(r, &packetRemaining)
	}
	if msg.UsernameFlag {
		msg.Username = getString(r, &packetRemaining)
	}
	if msg.PasswordFlag {
		msg.Password = getString(r, &packetRemaining)
	}

	return nil
}

type ConnAck struct {
	Header
	ReturnCode ReturnCode
}

func (msg *ConnAck) Encode(w io.Writer) (err error) {
	buf := new(bytes.Buffer)

	buf.WriteByte(byte(0))
	setUint8(uint8(msg.ReturnCode), buf)

	return writeMessage(w, &msg.Header, buf)
}

func (msg *ConnAck) Decode(r io.Reader, hdr Header, packetRemaining int32) (err error) {
	defer func() {
		err = recoverError(err)
	}()

	getUint8(r, &packetRemaining) // Skip reserved byte.
	msg.ReturnCode = ReturnCode(getUint8(r, &packetRemaining))
	if !msg.ReturnCode.IsValid() {
		return badReturnCodeError
	}

	return nil
}

type Publish struct {
	Header
	TopicName string
	MessageId uint16
	Data []byte
}

func (msg *Publish) Encode(w io.Writer) (err error) {
	buf := new(bytes.Buffer)

	setString(msg.TopicName, buf)
	if msg.Header.QosLevel.HasId() {
		setUint16(msg.MessageId, buf)
	}
	buf.Write(msg.Data)

	return writeMessage(w, &msg.Header, buf)
}

func (msg *Publish) Decode(r io.Reader, hdr Header, packetRemaining int32) (err error) {
	defer func() {
		err = recoverError(err)
	}()

	msg.TopicName = getString(r, &packetRemaining)
	if msg.Header.QosLevel.HasId() {
		msg.MessageId = getUint16(r, &packetRemaining)
	}
	msg.Data = make([]byte, packetRemaining)
	if _, err = io.ReadFull(r, msg.Data); err != nil {
		return err
	}
	return nil
}

type PubAck struct {
	AckCommon
}

type PubRec struct {
	AckCommon
}

type PubRel struct {
	AckCommon
}

type PubComp struct {
	AckCommon
}

type Subscribe struct {
	Header
	MessageId uint16
	Topics []string
	TopicsQos []QosLevel
}

func (msg *Subscribe) Encode(w io.Writer) (err error) {
	buf := new(bytes.Buffer)
	if msg.Header.QosLevel.HasId() {
		setUint16(msg.MessageId, buf)
	}
	for i := 0; i < len(msg.Topics); i += 1 {
		setString(msg.Topics[i], buf)
		setUint8(uint8(msg.TopicsQos[i]), buf)
	}

	return writeMessage(w, &msg.Header, buf)
}

func (msg *Subscribe) Decode(r io.Reader, hdr Header, packetRemaining int32) (err error) {
	defer func() {
		err = recoverError(err)
	}()

	if msg.Header.QosLevel.HasId() {
		msg.MessageId = getUint16(r, &packetRemaining)
	}
	topics := make([]string, 0)
	topicsQos := make([]QosLevel, 0)
	for packetRemaining > 0 {
		topics = append(topics, getString(r, &packetRemaining))
		topicsQos = append(topicsQos, QosLevel(getUint8(r, &packetRemaining)))
	}
	msg.Topics = topics
	msg.TopicsQos = topicsQos

	return nil
}

type SubAck struct {
	Header
	MessageId uint16
	TopicsQos []QosLevel
}

func (msg *SubAck) Encode(w io.Writer) (err error) {
	buf := new(bytes.Buffer)
	setUint16(msg.MessageId, buf)
	for i := 0; i < len(msg.TopicsQos); i += 1 {
		setUint8(uint8(msg.TopicsQos[i]), buf)
	}

	return writeMessage(w, &msg.Header, buf)
}

func (msg *SubAck) Decode(r io.Reader, hdr Header, packetRemaining int32) (err error) {
	defer func() {
		err = recoverError(err)
	}()

	msg.MessageId = getUint16(r, &packetRemaining)
	topicsQos := make([]QosLevel, 0)
	for packetRemaining > 0 {
		grantedQos := QosLevel(getUint8(r, &packetRemaining) & 0x03)
		topicsQos = append(topicsQos, grantedQos)
	}
	msg.TopicsQos = topicsQos

	return nil
}

type Unsubscribe struct {
	Header
	MessageId uint16
	Topics []string
}

func (msg *Unsubscribe) Encode(w io.Writer) (err error) {
	buf := new(bytes.Buffer)
	if msg.Header.QosLevel.HasId() {
		setUint16(msg.MessageId, buf)
	}
	for _, topic := range msg.Topics {
		setString(topic, buf)
	}

	return writeMessage(w, &msg.Header, buf)
}

func (msg *Unsubscribe) Decode(r io.Reader, hdr Header, packetRemaining int32) (err error) {
	defer func() {
		err = recoverError(err)
	}()

	if qos := msg.Header.QosLevel; qos == 1 || qos == 2 {
		msg.MessageId = getUint16(r, &packetRemaining)
	}
	topics := make([]string, 0)
	for packetRemaining > 0 {
		topics = append(topics, getString(r, &packetRemaining))
	}
	msg.Topics = topics

	return nil
}

type UnsubAck struct {
	AckCommon
}

type AckCommon struct {
	Header
	MessageId uint16
}

func (msg *AckCommon) Encode(w io.Writer) (err error) {
	buf := new(bytes.Buffer)
	setUint16(msg.MessageId, buf)

	return writeMessage(w, &msg.Header, buf)
}

func (msg *AckCommon) Decode(r io.Reader, hdr Header, packetRemaining int32) (err error) {
	defer func() {
		err = recoverError(err)
	}()

	msg.MessageId = getUint16(r, &packetRemaining)

	return nil
}
