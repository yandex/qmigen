package main

// LM940 QMI Command Reference Guide, Section 3.1, Table 3-1
type Service uint8

const (
	QMI_SERVICE_UNKNOWN Service = 0xff

	QMI_SERVICE_CTL   = 0
	QMI_SERVICE_WDS   = 1
	QMI_SERVICE_DMS   = 2
	QMI_SERVICE_NAS   = 3
	QMI_SERVICE_QOS   = 4
	QMI_SERVICE_WMS   = 5
	QMI_SERVICE_PDS   = 6
	QMI_SERVICE_AUTH  = 7
	QMI_SERVICE_AT    = 8
	QMI_SERVICE_VOICE = 9
	QMI_SERVICE_CAT2  = 10
	QMI_SERVICE_UIM   = 11
	QMI_SERVICE_PBM   = 12
	QMI_SERVICE_QCHAT = 13
	QMI_SERVICE_RMTFS = 14
	QMI_SERVICE_TEST  = 15
	QMI_SERVICE_LOC   = 16
	QMI_SERVICE_SAR   = 17
	QMI_SERVICE_IMS   = 18
	QMI_SERVICE_ADC   = 19
	QMI_SERVICE_CSD   = 20
	QMI_SERVICE_MFS   = 21
	QMI_SERVICE_TIME  = 22
	QMI_SERVICE_TS    = 23
	QMI_SERVICE_TMD   = 24
	QMI_SERVICE_SAP   = 25
	QMI_SERVICE_WDA   = 26
	QMI_SERVICE_TSYNC = 27
	QMI_SERVICE_RFSA  = 28
	QMI_SERVICE_CSVT  = 29
	QMI_SERVICE_QCMAP = 30
	QMI_SERVICE_IMSP  = 31
	QMI_SERVICE_IMSVT = 32
	QMI_SERVICE_IMSA  = 33
	QMI_SERVICE_COEX  = 34
	// 35: reserved
	QMI_SERVICE_PDC = 36
	// 37: reserved
	QMI_SERVICE_STX    = 38
	QMI_SERVICE_BIT    = 39
	QMI_SERVICE_IMSRTP = 40
	QMI_SERVICE_RFRPE  = 41
	QMI_SERVICE_DSD    = 42
	QMI_SERVICE_SSCTL  = 43

	QMI_SERVICE_GMS = 231 // Telit

	QMI_SERVICE_CAT = 224
	QMI_SERVICE_RMS = 225
	QMI_SERVICE_OMA = 226
)

var ServiceMap = map[Service]string{
	0:   "CTL",
	1:   "WDS",
	2:   "DMS",
	3:   "NAS",
	4:   "QOS",
	5:   "WMS",
	6:   "PDS",
	7:   "AUTH",
	8:   "AT",
	9:   "VOICE",
	10:  "CAT2",
	11:  "UIM",
	12:  "PBM",
	13:  "QCHAT",
	14:  "RMTFS",
	15:  "TEST",
	16:  "LOC",
	17:  "SAR",
	18:  "IMS",
	19:  "ADC",
	20:  "CSD",
	21:  "MFS",
	22:  "TIME",
	23:  "TS",
	24:  "TMD",
	25:  "SAP",
	26:  "WDA",
	27:  "TSYNC",
	28:  "RFSA",
	29:  "CSVT",
	30:  "QCMAP",
	31:  "IMSP",
	32:  "IMSVT",
	33:  "IMSA",
	34:  "COEX",
	36:  "PDC",
	38:  "STX",
	39:  "BIT",
	40:  "IMSRTP",
	41:  "RFRPE",
	42:  "DSD",
	43:  "SSCTL",
	231: "GMS",
	224: "CAT",
	225: "RMS",
	226: "OMA",
}

const COMMON_FOOTER = `
type QMIService interface {
	ServiceID() Service
}

type QMIOperation interface {
	OperationResult() QMIStructOperationResult
}

type Message interface {
	ServiceID() Service
	MessageID() uint16
	TLVsWriteTo(io.Writer) error
	TLVsReadFrom(*bytes.Buffer) error
}

type Device struct {
	f    *os.File
	name string

	ch      map[uint32]chan Message
	clients map[Service]*Client

	ctx    context.Context
	cancel context.CancelFunc
	err    error

	sync.Mutex
}

type Service uint8

func (s Service) String() string {
	if desc := ServiceMap[s]; desc != "" {
		return fmt.Sprintf("Service %s", desc)
	} else {
		return fmt.Sprintf("Unknown service %x", uint8(s))
	}
}

func findTag(r *bytes.Buffer, tag uint8) *bytes.Buffer {
	b := r.Bytes()
	for i := 0; i+3 < r.Len(); {
		t := b[i]
		l := binary.LittleEndian.Uint16(b[i+1:])
		i += 3
		if r.Len()-i >= int(l) {
			if t == tag {
				buf := &bytes.Buffer{}
				buf.Write(b[i : i+int(l)])
				return buf
			} else {
				i += int(l)
			}
		} else {
			break
		}
	}

	return nil
}

type Client struct {
	Device        *Device
	ClientID      uint8
	Service       Service
	TransactionID uint16

	sync.Mutex
}

func Open(name string) (*Device, error) {
	f, err := os.OpenFile(name, os.O_RDWR|os.O_EXCL|syscall.O_NOCTTY, 0600)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	dev := &Device{
		f:       f,
		name:    name,
		ctx:     ctx,
		cancel:  cancel,
		ch:      make(map[uint32]chan Message),
		clients: make(map[Service]*Client),
	}

	dev.clients[QMI_SERVICE_CTL] = &Client{
		Device:   dev,
		ClientID: 0,
		Service:  QMI_SERVICE_CTL,
	}

	go dev.reader()

	ctl, _ := dev.GetService(QMI_SERVICE_CTL)
	_, err = ctl.Send(&CTLSyncInput{})
	if err != nil {
		return nil, err
	}

	return dev, nil
}

type ErrAlreadyClosed string

func (e ErrAlreadyClosed) Error() string {
	return fmt.Sprintf("device %s is already closed", string(e))
}

var TLVConstructors = map[Service]map[uint16]func() Message{}

func registerMessage(f func() Message) {
	m := f()
	msgs, ok := TLVConstructors[m.ServiceID()]
	if !ok {
		msgs = make(map[uint16]func() Message)
		TLVConstructors[m.ServiceID()] = msgs
	}
	msgs[m.MessageID()] = f
}

type ErrBadMarker byte

func (e ErrBadMarker) Error() string {
	return fmt.Sprintf("bad marker: %x != 1", byte(e))
}

type ErrBadService Service

func (e ErrBadService) Error() string {
	return fmt.Sprintf("unexpected ServiceID: %x", byte(e))
}

type ErrBadMessage uint16

func (e ErrBadMessage) Error() string {
	return fmt.Sprintf("unexpected MessageID: %x", uint16(e))
}

func Unmarshal(buf []byte, dst *Message) (uint32, error) {
	if len(buf) < 12 {
		return 0, io.ErrUnexpectedEOF
	}

	if buf[0] != 1 {
		return 0, ErrBadMarker(buf[0])
	}

	qmuxlen := binary.LittleEndian.Uint16(buf[1:3])
	if qmuxlen > uint16(len(buf)-1) {
		return 0, io.ErrUnexpectedEOF
	}

	buf = buf[0 : qmuxlen+1]

	svcid := Service(buf[4])
	msgs, ok := TLVConstructors[svcid]
	if !ok {
		return 0, ErrBadService(svcid)
	}

	var is_normal_svc int
	var txid uint16
	if svcid == QMI_SERVICE_CTL {
		is_normal_svc = 0
		txid = uint16(buf[7])
	} else {
		is_normal_svc = 1
		txid = binary.LittleEndian.Uint16(buf[7:9])
	}

	msgid := binary.LittleEndian.Uint16(buf[8+is_normal_svc:])
	cons, ok := msgs[msgid]
	if !ok {
		return 0, ErrBadMessage(msgid)
	}

	tlvlen := binary.LittleEndian.Uint16(buf[10+is_normal_svc:])

	result := cons()
	tlvs := buf[12+is_normal_svc : 12+is_normal_svc+int(tlvlen)]
	b := bytes.NewBuffer(tlvs)
	result.TLVsReadFrom(b)
	*dst = result

	return uint32(buf[5]) | uint32(txid)<<8, nil
}

func (dev *Device) reader() {
	var msg Message
	var cid uint32

	buf := make([]byte, 2048)
	offset := 0

	for {
		select {
		case <-dev.ctx.Done():
			return
		default:
		}

		n, err := dev.f.Read(buf[offset:])
		if err != nil {
			dev.err = err
			dev.Close()
			return
		}

		if buf[offset] != 1 {
			offset = 0
		} else {
			offset += n
		}

		cid, err = Unmarshal(buf[0:offset], &msg)
		if err == io.EOF {
			continue
		} else if err == nil {
			dev.Lock()
			ch := dev.ch[cid]
			dev.Unlock()

			if ch != nil {
				ch <- msg
			}
		} else {
			log.Printf("Unmarshal failed: %s", err)
		}

		offset = 0
	}
}

func (dev *Device) Close() error {
	if dev.f == nil {
		return ErrAlreadyClosed(dev.name)
	}

	err := dev.f.Close()
	if err != nil {
		return err
	}

	dev.cancel()
	dev.f = nil
	dev.clients = nil
	return nil
}

func (dev *Device) GetService(service Service) (*Client, error) {
	dev.Lock()
	client, ok := dev.clients[service]
	dev.Unlock()

	if ok {
		return client, nil
	}

	client = &Client{
		Device:  dev,
		Service: service,
	}

	ctl, _ := dev.GetService(QMI_SERVICE_CTL)
	resp, err := ctl.Send(&CTLAllocateCIDInput{Service: uint8(service)})
	if err != nil {
		return nil, err
	}

	client.ClientID = resp.(*CTLAllocateCIDOutput).AllocationInfo.Cid

	dev.Lock()
	dev.clients[service] = client
	dev.Unlock()

	return client, nil
}

func (dev *Device) Send(m Message) (resp Message, err error) {
	client, err := dev.GetService(m.ServiceID())
	if err != nil {
		return nil, err
	}

	return client.Send(m)
}

func (client *Client) Send(m Message) (resp Message, err error) {
	if client.Device.f == nil {
		err = ErrAlreadyClosed(client.Device.name)
		return
	}

	client.Lock()
	client.TransactionID += 1
	cid := uint32(client.ClientID) | uint32(client.TransactionID)<<8
	client.Unlock()

	client.Device.Lock()
	ch_ := client.Device.ch[cid]
	ch := make(chan Message, 1)
	client.Device.ch[cid] = ch
	client.Device.Unlock()

	if ch_ != nil {
		panic(fmt.Sprintf(
			"dev %s: race @ cid %x",
			client.Device.name,
			cid,
		))
	}

	svc := m.ServiceID()
	var is_normal_svc int
	if svc != QMI_SERVICE_CTL {
		is_normal_svc = 1
	}
	tlv_buf := &bytes.Buffer{}
	m.TLVsWriteTo(tlv_buf)

	buf := &bytes.Buffer{}
	buf.Write([]byte{1}) // marker
	binary.Write(buf, binary.LittleEndian, uint16(tlv_buf.Len()+11+is_normal_svc))
	buf.Write([]byte{0, uint8(svc), client.ClientID, 0})

	if svc != QMI_SERVICE_CTL {
		binary.Write(buf, binary.LittleEndian, client.TransactionID)
	} else {
		buf.Write([]byte{uint8(client.TransactionID & 0xff)})
	}
	binary.Write(buf, binary.LittleEndian, m.MessageID())
	binary.Write(buf, binary.LittleEndian, uint16(tlv_buf.Len()))

	_, err = tlv_buf.WriteTo(buf)
	if err != nil {
		return
	}

	_, err = buf.WriteTo(client.Device.f)
	if err != nil {
		return
	}

	resp = <-ch

	client.Device.Lock()
	close(ch)
	delete(client.Device.ch, cid)
	client.Device.Unlock()

	op, ok := resp.(QMIOperation)
	if ok {
		op_result := op.OperationResult()
		if op_result.ErrorStatus != 0 {
			resp = nil
			err = QMIError(op_result.ErrorCode)
		}
	}

	return
}

// LM940 QMI Command Reference Guide, Section 3.2.1, Table 3-2; Section 4.1.3.3
const (
	QMI_RESULT_SUCCESS = 0
	QMI_RESULT_FAILURE = 1
)

// LM940 QMI Command Reference Guide, Section 3.2.1, Table 3-3; Section 4.1.3.3
type QMIError uint16

const (
	QMI_PROTOCOL_ERROR_NONE                    QMIError = 0
	QMI_PROTOCOL_ERROR_MALFORMED_MESSAGE                = 1
	QMI_PROTOCOL_ERROR_NO_MEMORY                        = 2
	QMI_PROTOCOL_ERROR_INTERNAL                         = 3
	QMI_PROTOCOL_ERROR_ABORTED                          = 4
	QMI_PROTOCOL_ERROR_CLIENT_IDS_EXHAUSTED             = 5
	QMI_PROTOCOL_ERROR_UNABORTABLE_TRANSACTION          = 6
	QMI_PROTOCOL_ERROR_INVALID_CLIENT_ID                = 7
	QMI_PROTOCOL_ERROR_NO_THRESHOLDS_PROVIDED           = 8
	QMI_PROTOCOL_ERROR_INVALID_HANDLE                   = 9
	QMI_PROTOCOL_ERROR_INVALID_PROFILE                  = 10
	QMI_PROTOCOL_ERROR_INVALID_PIN_ID                   = 11
	QMI_PROTOCOL_ERROR_INCORRECT_PIN                    = 12
	QMI_PROTOCOL_ERROR_NO_NETWORK_FOUND                 = 13
	QMI_PROTOCOL_ERROR_CALL_FAILED                      = 14
	QMI_PROTOCOL_ERROR_OUT_OF_CALL                      = 15
	QMI_PROTOCOL_ERROR_NOT_PROVISIONED                  = 16
	QMI_PROTOCOL_ERROR_MISSING_ARGUMENT                 = 17
	// 18: reserved
	QMI_PROTOCOL_ERROR_ARGUMENT_TOO_LONG = 19
	// 20: reserved
	// 21: reserved
	QMI_PROTOCOL_ERROR_INVALID_TRANSACTION_ID        = 22
	QMI_PROTOCOL_ERROR_DEVICE_IN_USE                 = 23
	QMI_PROTOCOL_ERROR_NETWORK_UNSUPPORTED           = 24
	QMI_PROTOCOL_ERROR_DEVICE_UNSUPPORTED            = 25
	QMI_PROTOCOL_ERROR_NO_EFFECT                     = 26
	QMI_PROTOCOL_ERROR_NO_FREE_PROFILE               = 27
	QMI_PROTOCOL_ERROR_INVALID_PDP_TYPE              = 28
	QMI_PROTOCOL_ERROR_INVALID_TECHNOLOGY_PREFERENCE = 29
	QMI_PROTOCOL_ERROR_INVALID_PROFILE_TYPE          = 30
	QMI_PROTOCOL_ERROR_INVALID_SERVICE_TYPE          = 31
	QMI_PROTOCOL_ERROR_INVALID_REGISTER_ACTION       = 32
	QMI_PROTOCOL_ERROR_INVALID_PS_ATTACH_ACTION      = 33
	QMI_PROTOCOL_ERROR_AUTHENTICATION_FAILED         = 34
	QMI_PROTOCOL_ERROR_PIN_BLOCKED                   = 35
	QMI_PROTOCOL_ERROR_PIN_ALWAYS_BLOCKED            = 36
	QMI_PROTOCOL_ERROR_UIM_UNINITIALIZED             = 37
	QMI_PROTOCOL_ERROR_MAXIMUM_QOS_REQUESTS_IN_USE   = 38
	QMI_PROTOCOL_ERROR_INCORRECT_FLOW_FILTER         = 39
	QMI_PROTOCOL_ERROR_NETWORK_QOS_UNAWARE           = 40
	QMI_PROTOCOL_ERROR_INVALID_QOS_ID                = 41
	QMI_PROTOCOL_ERROR_QOS_UNAVAILABLE               = 42
	QMI_PROTOCOL_ERROR_FLOW_SUSPENDED                = 43
	// 44: reserved
	// 45: reserved
	QMI_PROTOCOL_ERROR_GENERAL_ERROR                = 46
	QMI_PROTOCOL_ERROR_UNKNOWN_ERROR                = 47
	QMI_PROTOCOL_ERROR_INVALID_ARGUMENT             = 48
	QMI_PROTOCOL_ERROR_INVALID_INDEX                = 49
	QMI_PROTOCOL_ERROR_NO_ENTRY                     = 50
	QMI_PROTOCOL_ERROR_DEVICE_STORAGE_FULL          = 51
	QMI_PROTOCOL_ERROR_DEVICE_NOT_READY             = 52
	QMI_PROTOCOL_ERROR_NETWORK_NOT_READY            = 53
	QMI_PROTOCOL_ERROR_WMS_CAUSE_CODE               = 54
	QMI_PROTOCOL_ERROR_WMS_MESSAGE_NOT_SENT         = 55
	QMI_PROTOCOL_ERROR_WMS_MESSAGE_DELIVERY_FAILURE = 56
	QMI_PROTOCOL_ERROR_WMS_INVALID_MESSAGE_ID       = 57
	QMI_PROTOCOL_ERROR_WMS_ENCODING                 = 58
	QMI_PROTOCOL_ERROR_AUTHENTICATION_LOCK          = 59
	QMI_PROTOCOL_ERROR_INVALID_TRANSITION           = 60
	// 61-64: reserved
	QMI_PROTOCOL_ERROR_SESSION_INACTIVE        = 65
	QMI_PROTOCOL_ERROR_SESSION_INVALID         = 66
	QMI_PROTOCOL_ERROR_SESSION_OWNERSHIP       = 67
	QMI_PROTOCOL_ERROR_INSUFFICIENT_RESOURCES  = 68
	QMI_PROTOCOL_ERROR_DISABLED                = 69
	QMI_PROTOCOL_ERROR_INVALID_OPERATION       = 70
	QMI_PROTOCOL_ERROR_INVALID_QMI_COMMAND     = 71
	QMI_PROTOCOL_ERROR_WMS_T_PDU_TYPE          = 72
	QMI_PROTOCOL_ERROR_WMS_SMSC_ADDRESS        = 73
	QMI_PROTOCOL_ERROR_INFORMATION_UNAVAILABLE = 74
	QMI_PROTOCOL_ERROR_SEGMENT_TOO_LONG        = 75
	QMI_PROTOCOL_ERROR_SEGMENT_ORDER           = 76
	QMI_PROTOCOL_ERROR_BUNDLING_NOT_SUPPORTED  = 77
	QMI_PROTOCOL_ERROR_POLICY_MISMATCH         = 79
	QMI_PROTOCOL_ERROR_SIM_FILE_NOT_FOUND      = 80
	QMI_PROTOCOL_ERROR_EXTENDED_INTERNAL       = 81
	QMI_PROTOCOL_ERROR_ACCESS_DENIED           = 82
	QMI_PROTOCOL_ERROR_HARDWARE_RESTRICTED     = 83
	QMI_PROTOCOL_ERROR_ACK_NOT_SENT            = 84
	QMI_PROTOCOL_ERROR_INJECT_TIMEOUT          = 85
	// 86-89: reserved
	QMI_PROTOCOL_ERROR_INCOMPATIBLE_STATE       = 90
	QMI_PROTOCOL_ERROR_FDN_RESTRICT             = 91
	QMI_PROTOCOL_ERROR_SUPS_FAILURE_CASE        = 92
	QMI_PROTOCOL_ERROR_NO_RADIO                 = 93
	QMI_PROTOCOL_ERROR_NOT_SUPPORTED            = 94
	QMI_PROTOCOL_ERROR_NO_SUBSCRIPTION          = 95
	QMI_PROTOCOL_ERROR_CARD_CALL_CONTROL_FAILED = 96
	QMI_PROTOCOL_ERROR_NETWORK_ABORTED          = 97
	QMI_PROTOCOL_ERROR_MSG_BLOCKED              = 98
	// 99: reserved
	QMI_PROTOCOL_ERROR_INVALID_SESSION_TYPE      = 100
	QMI_PROTOCOL_ERROR_INVALID_PB_TYPE           = 101
	QMI_PROTOCOL_ERROR_NO_SIM                    = 102
	QMI_PROTOCOL_ERROR_PB_NOT_READY              = 103
	QMI_PROTOCOL_ERROR_PIN_RESTRICTION           = 104
	QMI_PROTOCOL_ERROR_PIN2_RESTRICTION          = 105
	QMI_PROTOCOL_ERROR_PUK_RESTRICTION           = 106
	QMI_PROTOCOL_ERROR_PUK2_RESTRICTION          = 107
	QMI_PROTOCOL_ERROR_PB_ACCESS_RESTRICTED      = 108
	QMI_PROTOCOL_ERROR_PB_TEXT_TOO_LONG          = 109
	QMI_PROTOCOL_ERROR_PB_NUMBER_TOO_LONG        = 110
	QMI_PROTOCOL_ERROR_PB_HIDDEN_KEY_RESTRICTION = 111

	QMI_PROTOCOL_ERROR_CAT_EVENT_REGISTRATION_FAILED = 0xF001
	QMI_PROTOCOL_ERROR_CAT_INVALID_TERMINAL_RESPONSE = 0xF002
	QMI_PROTOCOL_ERROR_CAT_INVALID_ENVELOPE_COMMAND  = 0xF003
	QMI_PROTOCOL_ERROR_CAT_ENVELOPE_COMMAND_BUSY     = 0xF004
	QMI_PROTOCOL_ERROR_CAT_ENVELOPE_COMMAND_FAILED   = 0xF005
)

var QMIErrorDescription = map[QMIError]string{
	QMI_PROTOCOL_ERROR_NONE:                          "No error",
	QMI_PROTOCOL_ERROR_MALFORMED_MESSAGE:             "Malformed message",
	QMI_PROTOCOL_ERROR_NO_MEMORY:                     "No memory",
	QMI_PROTOCOL_ERROR_INTERNAL:                      "Internal",
	QMI_PROTOCOL_ERROR_ABORTED:                       "Aborted",
	QMI_PROTOCOL_ERROR_CLIENT_IDS_EXHAUSTED:          "Client IDs exhausted",
	QMI_PROTOCOL_ERROR_UNABORTABLE_TRANSACTION:       "Unabortable transaction",
	QMI_PROTOCOL_ERROR_INVALID_CLIENT_ID:             "Invalid client ID",
	QMI_PROTOCOL_ERROR_NO_THRESHOLDS_PROVIDED:        "No thresholds provided",
	QMI_PROTOCOL_ERROR_INVALID_HANDLE:                "Invalid handle",
	QMI_PROTOCOL_ERROR_INVALID_PROFILE:               "Invalid profile",
	QMI_PROTOCOL_ERROR_INVALID_PIN_ID:                "Invalid PIN ID",
	QMI_PROTOCOL_ERROR_INCORRECT_PIN:                 "Incorrect PIN",
	QMI_PROTOCOL_ERROR_NO_NETWORK_FOUND:              "No network found",
	QMI_PROTOCOL_ERROR_CALL_FAILED:                   "Call failed",
	QMI_PROTOCOL_ERROR_OUT_OF_CALL:                   "Out of call",
	QMI_PROTOCOL_ERROR_NOT_PROVISIONED:               "Not provisioned",
	QMI_PROTOCOL_ERROR_MISSING_ARGUMENT:              "Missing argument",
	QMI_PROTOCOL_ERROR_ARGUMENT_TOO_LONG:             "Argument too long",
	QMI_PROTOCOL_ERROR_INVALID_TRANSACTION_ID:        "Invalid transaction ID",
	QMI_PROTOCOL_ERROR_DEVICE_IN_USE:                 "Device in use",
	QMI_PROTOCOL_ERROR_NETWORK_UNSUPPORTED:           "Network unsupported",
	QMI_PROTOCOL_ERROR_DEVICE_UNSUPPORTED:            "Device unsupported",
	QMI_PROTOCOL_ERROR_NO_EFFECT:                     "No effect",
	QMI_PROTOCOL_ERROR_NO_FREE_PROFILE:               "No free profile",
	QMI_PROTOCOL_ERROR_INVALID_PDP_TYPE:              "Invalid PDP type",
	QMI_PROTOCOL_ERROR_INVALID_TECHNOLOGY_PREFERENCE: "Invalid technology preference",
	QMI_PROTOCOL_ERROR_INVALID_PROFILE_TYPE:          "Invalid profile type",
	QMI_PROTOCOL_ERROR_INVALID_SERVICE_TYPE:          "Invalid service type",
	QMI_PROTOCOL_ERROR_INVALID_REGISTER_ACTION:       "Invalid register action",
	QMI_PROTOCOL_ERROR_INVALID_PS_ATTACH_ACTION:      "Invalid PS attach action",
	QMI_PROTOCOL_ERROR_AUTHENTICATION_FAILED:         "Authentication failed",
	QMI_PROTOCOL_ERROR_PIN_BLOCKED:                   "PIN blocked",
	QMI_PROTOCOL_ERROR_PIN_ALWAYS_BLOCKED:            "PIN always blocked",
	QMI_PROTOCOL_ERROR_UIM_UNINITIALIZED:             "UIM uninitialized",
	QMI_PROTOCOL_ERROR_MAXIMUM_QOS_REQUESTS_IN_USE:   "Maximum QoS requests in use",
	QMI_PROTOCOL_ERROR_INCORRECT_FLOW_FILTER:         "Incorrect flow filter",
	QMI_PROTOCOL_ERROR_NETWORK_QOS_UNAWARE:           "Network QoS unaware",
	QMI_PROTOCOL_ERROR_INVALID_QOS_ID:                "Invalid QoS ID",
	QMI_PROTOCOL_ERROR_QOS_UNAVAILABLE:               "QoS unavailable",
	QMI_PROTOCOL_ERROR_FLOW_SUSPENDED:                "Flow suspended",
	QMI_PROTOCOL_ERROR_GENERAL_ERROR:                 "General error",
	QMI_PROTOCOL_ERROR_UNKNOWN_ERROR:                 "Unknown error",
	QMI_PROTOCOL_ERROR_INVALID_ARGUMENT:              "Invalid argument",
	QMI_PROTOCOL_ERROR_INVALID_INDEX:                 "Invalid index",
	QMI_PROTOCOL_ERROR_NO_ENTRY:                      "No entry",
	QMI_PROTOCOL_ERROR_DEVICE_STORAGE_FULL:           "Device storage full",
	QMI_PROTOCOL_ERROR_DEVICE_NOT_READY:              "Device not ready",
	QMI_PROTOCOL_ERROR_NETWORK_NOT_READY:             "Network not ready",
	QMI_PROTOCOL_ERROR_WMS_CAUSE_CODE:                "WMS cause code",
	QMI_PROTOCOL_ERROR_WMS_MESSAGE_NOT_SENT:          "WMS message not sent",
	QMI_PROTOCOL_ERROR_WMS_MESSAGE_DELIVERY_FAILURE:  "WMS message delivery failure",
	QMI_PROTOCOL_ERROR_WMS_INVALID_MESSAGE_ID:        "WMS invalid message ID",
	QMI_PROTOCOL_ERROR_WMS_ENCODING:                  "WMS encoding",
	QMI_PROTOCOL_ERROR_AUTHENTICATION_LOCK:           "Authentication lock",
	QMI_PROTOCOL_ERROR_INVALID_TRANSITION:            "Invalid transition",
	QMI_PROTOCOL_ERROR_SESSION_INACTIVE:              "Session inactive",
	QMI_PROTOCOL_ERROR_SESSION_INVALID:               "Session invalid",
	QMI_PROTOCOL_ERROR_SESSION_OWNERSHIP:             "Session ownership",
	QMI_PROTOCOL_ERROR_INSUFFICIENT_RESOURCES:        "Insufficient resources",
	QMI_PROTOCOL_ERROR_DISABLED:                      "Disabled",
	QMI_PROTOCOL_ERROR_INVALID_OPERATION:             "Invalid operation",
	QMI_PROTOCOL_ERROR_INVALID_QMI_COMMAND:           "Invalid QMI command",
	QMI_PROTOCOL_ERROR_WMS_T_PDU_TYPE:                "WMS T-PDU type",
	QMI_PROTOCOL_ERROR_WMS_SMSC_ADDRESS:              "WMS SMSC address",
	QMI_PROTOCOL_ERROR_INFORMATION_UNAVAILABLE:       "Information unavailable",
	QMI_PROTOCOL_ERROR_SEGMENT_TOO_LONG:              "Segment too long",
	QMI_PROTOCOL_ERROR_SEGMENT_ORDER:                 "Segment order",
	QMI_PROTOCOL_ERROR_BUNDLING_NOT_SUPPORTED:        "Bundling not supported",
	QMI_PROTOCOL_ERROR_POLICY_MISMATCH:               "Policy mismatch",
	QMI_PROTOCOL_ERROR_SIM_FILE_NOT_FOUND:            "SIM file not found",
	QMI_PROTOCOL_ERROR_EXTENDED_INTERNAL:             "Extended internal error",
	QMI_PROTOCOL_ERROR_ACCESS_DENIED:                 "Access denied",
	QMI_PROTOCOL_ERROR_HARDWARE_RESTRICTED:           "Hardware restricted",
	QMI_PROTOCOL_ERROR_ACK_NOT_SENT:                  "ACK not sent",
	QMI_PROTOCOL_ERROR_INJECT_TIMEOUT:                "Inject timeout",
	QMI_PROTOCOL_ERROR_INCOMPATIBLE_STATE:            "Incompatible state",
	QMI_PROTOCOL_ERROR_FDN_RESTRICT:                  "FDN restrict",
	QMI_PROTOCOL_ERROR_SUPS_FAILURE_CASE:             "SUPS failure case",
	QMI_PROTOCOL_ERROR_NO_RADIO:                      "No radio",
	QMI_PROTOCOL_ERROR_NOT_SUPPORTED:                 "Not supported",
	QMI_PROTOCOL_ERROR_NO_SUBSCRIPTION:               "No subscription",
	QMI_PROTOCOL_ERROR_CARD_CALL_CONTROL_FAILED:      "Card call control failed",
	QMI_PROTOCOL_ERROR_NETWORK_ABORTED:               "Network aborted",
	QMI_PROTOCOL_ERROR_MSG_BLOCKED:                   "Message blocked",
	QMI_PROTOCOL_ERROR_INVALID_SESSION_TYPE:          "Invalid session type",
	QMI_PROTOCOL_ERROR_INVALID_PB_TYPE:               "Invalid PB type",
	QMI_PROTOCOL_ERROR_NO_SIM:                        "No SIM",
	QMI_PROTOCOL_ERROR_PB_NOT_READY:                  "PB not ready",
	QMI_PROTOCOL_ERROR_PIN_RESTRICTION:               "PIN restriction",
	QMI_PROTOCOL_ERROR_PIN2_RESTRICTION:              "PIN2 restriction",
	QMI_PROTOCOL_ERROR_PUK_RESTRICTION:               "PUK restriction",
	QMI_PROTOCOL_ERROR_PUK2_RESTRICTION:              "PUK2 restriction",
	QMI_PROTOCOL_ERROR_PB_ACCESS_RESTRICTED:          "PB access restricted",
	QMI_PROTOCOL_ERROR_PB_TEXT_TOO_LONG:              "PB text too long",
	QMI_PROTOCOL_ERROR_PB_NUMBER_TOO_LONG:            "PB number too long",
	QMI_PROTOCOL_ERROR_PB_HIDDEN_KEY_RESTRICTION:     "PB hidden key restriction",

	QMI_PROTOCOL_ERROR_CAT_EVENT_REGISTRATION_FAILED: "Event registration failed",
	QMI_PROTOCOL_ERROR_CAT_INVALID_TERMINAL_RESPONSE: "Invalid terminal response",
	QMI_PROTOCOL_ERROR_CAT_INVALID_ENVELOPE_COMMAND:  "Invalid envelope command",
	QMI_PROTOCOL_ERROR_CAT_ENVELOPE_COMMAND_BUSY:     "Envelope command busy",
	QMI_PROTOCOL_ERROR_CAT_ENVELOPE_COMMAND_FAILED:   "Envelope command failed",
}

func (qe QMIError) Error() string {
	desc := QMIErrorDescription[qe]
	if desc == "" {
		return "QMI Protocol Error: unknown error"
	} else {
		return "QMI Protocol Error: " + desc
	}
}

`

// vim: ai:ts=8:sw=8:noet:syntax=go
