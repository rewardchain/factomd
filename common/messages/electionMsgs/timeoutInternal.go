// Copyright 2017 Factom Foundation
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.

package electionMsgs

import (
	"fmt"
	"time"

	"github.com/FactomProject/factomd/common/constants"
	"github.com/FactomProject/factomd/common/interfaces"
	"github.com/FactomProject/factomd/common/messages/msgbase"
	"github.com/FactomProject/factomd/common/primitives"
	"github.com/FactomProject/factomd/elections"
	"github.com/FactomProject/factomd/state"
	log "github.com/sirupsen/logrus"
)

var _ = state.MakeMap

//General acknowledge message
type TimeoutInternal struct {
	msgbase.MessageBase
	Name        string
	SigType     bool // True for EOM, false for DBSig
	DBHeight    int
	Round       int
	MessageHash interfaces.IHash
}

var _ interfaces.IMsg = (*TimeoutInternal)(nil)
var _ interfaces.IElectionMsg = (*TimeoutInternal)(nil)

func (m *TimeoutInternal) MarshalBinary() (data []byte, err error) {
	var buf primitives.Buffer

	if err = buf.PushByte(constants.INTERNALTIMEOUT); err != nil {
		return nil, err
	}
	if e := buf.PushInt(int(m.DBHeight)); e != nil {
		return nil, e
	}
	if e := buf.PushByte(m.Minute); e != nil {
		return nil, e
	}
	if e := buf.PushByte(m.Minute); e != nil {
		return nil, e
	}
	data = buf.Bytes()
	return data, nil
}

func (m *TimeoutInternal) GetMsgHash() interfaces.IHash {
	if m.MsgHash == nil {
		data, err := m.MarshalBinary()
		if err != nil {
			return nil
		}
		m.MsgHash = primitives.Sha(data)
	}
	return m.MsgHash
}

// InitiateElectionAdapter will create a new election adapter if needed for the election message
func (m *TimeoutInternal) InitiateElectionAdapter(st interfaces.IState) bool {
	s := st.(*state.State)
	e := s.Elections.(*elections.Elections)

	msg := new(StartElectionInternal)
	msg.VMIndex = m.VMIndex
	msg.SetLocal(true)
	msg.DBHeight = uint32(m.DBHeight)
	msg.PreviousDBHash = st.GetDirectoryBlockByHeight(uint32(m.DBHeight - 1)).GetKeyMR()
	msg.VMIndex = m.VMIndex
	msg.Minute = m.Minute
	msg.SigType = m.SigType
	e.State.InMsgQueue().Enqueue(msg)

	// When we start a new election, we can process all messages that were being held
	go e.ProcessWaiting()
	return true
}

func (m *TimeoutInternal) ComparisonMinute() int {
	if !m.SigType {
		return -1
	}
	return int(m.Minute)
}

func (m *TimeoutInternal) ElectionProcess(is interfaces.IState, elect interfaces.IElections) {
	s := is.(*state.State)

	e, ok := elect.(*elections.Elections)
	if !ok {
		panic("Invalid elections object")
	}

	// We have advanced, so do nothing.  We can't reset anything because there
	// can be a timeout process that started before we got here (with short minutes)
	if e.DBHeight > m.DBHeight || e.ComparisonMinute() > m.ComparisonMinute() {
		return
	}

	// No election running, is there one we should start?
	if e.Electing == -1 || m.DBHeight > e.DBHeight || m.ComparisonMinute() > e.ComparisonMinute() {
		// When we are syncing this can happen, as we are syncing from disk quickly
		if uint32(e.DBHeight) < s.ProcessLists.DBHeightBase {
			return
		}
		servers := e.Federated
		nfeds := len(servers)
		VMscollected := make([]bool, nfeds, nfeds)
		for _, im := range e.Msgs {
			msg := im.(interfaces.IMsgAck)
			if int(msg.GetDBHeight()) == m.DBHeight && m.ComparisonMinute() == msg.(*EomSigInternal).ComparisonMinute() {
				VMscollected[msg.GetVMIndex()] = true
			}
		}
		found := false
		for i, b := range VMscollected {
			if !b {
				e.VMIndex = i
				found = true
				break
			}
		}

		if !found {
			// TODO: Set Electing to -1?
			return
		}

		e.Electing = state.MakeMap(nfeds, uint32(m.DBHeight))[e.Minute][e.VMIndex]

		elections.CheckAuthSetsMatch("TimeoutInternal.ElectionProcess", e, s)

		e.FedID = e.Federated[e.Electing].GetChainID()

		// Reset this value when we start an election
		for len(e.Round) <= e.Electing {
			e.Round = append(e.Round, 0)
		}
		e.Round[e.Electing] = 0

		e.LogPrintf("election", "**** Start an Election for %d[%x] ****", e.Electing, e.FedID.Bytes()[3:6])
		e.LogPrintf("faulting", "**** Start an Election for %d[%x] ****", e.Electing, e.FedID.Bytes()[3:6])

		// ------------------
		// Not in an election

		// Begin a new Election for a specific vm/min/height
		initiated := m.InitiateElectionAdapter(is)
		if !initiated {
			// True means the election is started or already going. False means it did not
			// start.
			// TODO: We should never get a false, so should we do something if it is?
		} // <-- Election Started
		return // Let the Election Start kick out the new timeout
	}

	// Operate in existing election

	e.State.(*state.State).Election2 = e.FeedBackStr("E", true, e.Electing)

	for len(e.Round) <= e.Electing {
		e.Round = append(e.Round, 0)
	}

	// New timeout, new round of elections.
	e.Round[e.Electing]++

	// If we don't have all our sync messages, we will have to come back around and see if all is well.
	// Start our timer to timeout this sync

	e.FaultId.Store(e.FaultId.Load() + 1) // increment the timeout counter
	go Fault(e, e.DBHeight, e.Minute, e.Round[e.Electing], e.FaultId.Load(), &e.FaultId, m.SigType, e.RoundTimeout)

	auditIdx := 0
	if len(e.Audit) > 0 {
		auditIdx = e.Round[e.Electing] % len(e.Audit) //e.AuditPriority()
	}
	// This server's possible identity as an audit server. -1 means we are not an audit server.
	aidx := e.AuditAdapterIndex(is.GetIdentityChainID()) //e.AuditIndex(is.GetIdentityChainID())

	if aidx >= 0 {
		serverMap := state.MakeMap(len(e.Federated), uint32(e.DBHeight))
		vm := state.FedServerVM(serverMap, len(e.Federated), e.Minute, e.Electing)

		if aidx == auditIdx {
			// Make consensus generate a volunteer message
			Sync := new(SyncMsg)
			Sync.SetLocal(true)
			Sync.VMIndex = vm
			Sync.TS = primitives.NewTimestampNow()
			Sync.Name = e.Name

			Sync.FedIdx = uint32(e.Electing)
			Sync.FedID = e.FedID

			actualidx := e.AuditIndex(is.GetIdentityChainID())
			Sync.ServerIdx = uint32(actualidx)
			Sync.ServerID = is.GetIdentityChainID()
			Sync.ServerName = is.GetFactomNodeName()

			Sync.Weight = primitives.Sha([]byte("Weight")) //e.APriority[auditIdx]
			Sync.DBHeight = uint32(e.DBHeight)
			Sync.Minute = byte(e.Minute)
			Sync.Round = e.Round[e.Electing]
			Sync.SigType = m.SigType
			s.InMsgQueue().Enqueue(Sync)
			s.Election2 = e.FeedBackStr(fmt.Sprintf("%d", e.Round[e.Electing]), false, auditIdx)
		}
	}

	if aidx != auditIdx {
		s.Election2 = e.FeedBackStr(fmt.Sprintf("%d-%d", e.Round[e.Electing], auditIdx), true, e.Electing)
	}

}

func (m *TimeoutInternal) GetServerID() interfaces.IHash {
	return nil
}

func (m *TimeoutInternal) LogFields() log.Fields {
	return log.Fields{"category": "message", "messagetype": "TimeoutInternal", "dbheight": m.DBHeight}
}

func (m *TimeoutInternal) GetRepeatHash() interfaces.IHash {
	return m.GetMsgHash()
}

// We have to return the hash of the underlying message.
func (m *TimeoutInternal) GetHash() interfaces.IHash {
	return m.MessageHash
}

func (m *TimeoutInternal) GetTimestamp() interfaces.Timestamp {
	return primitives.NewTimestampNow()
}

func (m *TimeoutInternal) Type() byte {
	return constants.INTERNALTIMEOUT
}

func (m *TimeoutInternal) Validate(state interfaces.IState) int {
	return 1
}

func (m *TimeoutInternal) ElectionValidate(ie interfaces.IElections) int {
	return 1
}

// Returns true if this is a message for this server to execute as
// a leader.
func (m *TimeoutInternal) ComputeVMIndex(state interfaces.IState) {
}

// Execute the leader functions of the given message
// Leader, follower, do the same thing.
func (m *TimeoutInternal) LeaderExecute(state interfaces.IState) {
	m.FollowerExecute(state)
}

func (m *TimeoutInternal) FollowerExecute(state interfaces.IState) {

}

// Acknowledgements do not go into the process list.
func (e *TimeoutInternal) Process(dbheight uint32, state interfaces.IState) bool {
	panic("Ack object should never have its Process() method called")
}

func (e *TimeoutInternal) JSONByte() ([]byte, error) {
	return primitives.EncodeJSON(e)
}

func (e *TimeoutInternal) JSONString() (string, error) {
	return primitives.EncodeJSONString(e)
}

func (m *TimeoutInternal) UnmarshalBinaryData(data []byte) (newData []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("Error unmarshalling: %v", r)
		}
	}()
	return
}

func (m *TimeoutInternal) UnmarshalBinary(data []byte) error {
	_, err := m.UnmarshalBinaryData(data)
	return err
}

func (m *TimeoutInternal) String() string {
	if m.LeaderChainID == nil {
		m.LeaderChainID = primitives.NewZeroHash()
	}
	return fmt.Sprintf(" %20s %10s dbheight %d minute %d",
		m.Name,
		"Time Out",
		m.DBHeight,
		m.Minute)
}

func (a *TimeoutInternal) IsSameAs(b *TimeoutInternal) bool {
	return true
}

func newRound(e *elections.Elections, dbheight int, minute int, round int) {

	time.Sleep(e.Timeout)
	if e.DBHeight > dbheight || e.Minute > minute {
		return
	}

	timeout := new(TimeoutInternal)
	timeout.Minute = byte(minute)
	timeout.DBHeight = dbheight
	timeout.Round = round
	e.Input.Enqueue(timeout)

}
