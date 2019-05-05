package rpc

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

type serverType int

const (
	OpHeartBeat = iota
	OpMasterInfo
	OpPing
	OpConnectToSlaves
	OpAddRootBlock
	OpGetEcoInfoList
	OpGetNextBlockToMine
	OpGetUnconfirmedHeaders
	OpGetAccountData
	OpAddTransaction
	OpAddMinorBlockHeader
	OpAddXshardTxList
	OpSyncMinorBlockList
	OpAddMinorBlock
	OpCreateClusterPeerConnection
	OpGetMinorBlock
	OpGetTransaction
	OpBatchAddXshardTxList
	OpExecuteTransaction
	OpGetTransactionReceipt
	OpGetMine
	OpGenTx
	OpGetTransactionListByAddress
	OpGetLogs
	OpEstimateGas
	OpGetStorageAt
	OpGetCode
	OpGasPrice
	OpGetWork
	OpSubmitWork
	// p2p api
	OpBroadcastNewTip
	OpBroadcastTransactions
	OpBroadcastMinorBlock
	OpGetMinorBlocks
	OpGetMinorBlockHeaders
	OpHandleNewTip
	OpAddTransactions

	MasterServer = serverType(1)
	SlaveServer  = serverType(0)

	timeOut = 10
)

var (
	// master apis
	masterApis = map[uint32]opType{
		OpAddMinorBlockHeader:   {name: "AddMinorBlockHeader"},
		OpBroadcastNewTip:       {name: "BroadcastNewTip"},
		OpBroadcastTransactions: {name: "BroadcastTransactions"},
		OpBroadcastMinorBlock:   {name: "BroadcastMinorBlock"},
		OpGetMinorBlocks:        {name: "GetMinorBlocks"},
		OpGetMinorBlockHeaders:  {name: "GetMinorBlockHeaders"},
	}
	// slave apis
	slaveApis = map[uint32]opType{
		OpHeartBeat:  {name: "HeartBeat"},
		OpMasterInfo: {name: "MasterInfo"},
		OpPing:       {name: "Ping"},
		OpAddRootBlock:                {name: "AddRootBlock"},
		OpGetEcoInfoList:              {name: "GetEcoInfoList"},
		OpGetNextBlockToMine:          {name: "GetNextBlockToMine"},
		OpGetUnconfirmedHeaders:       {name: "GetUnconfirmedHeaders"},
		OpGetAccountData:              {name: "GetAccountData"},
		OpAddTransaction:              {name: "AddTransaction"},
		OpAddXshardTxList:             {name: "AddXshardTxList"},
		OpSyncMinorBlockList:          {name: "SyncMinorBlockList"},
		OpAddMinorBlock:               {name: "AddMinorBlock"},
		OpCreateClusterPeerConnection: {name: "CreateClusterPeerConnection"},
		OpGetMinorBlock:               {name: "GetMinorBlock"},
		OpGetTransaction:              {name: "GetTransaction"},
		OpBatchAddXshardTxList:        {name: "BatchAddXshardTxList"},
		OpExecuteTransaction:          {name: "ExecuteTransaction"},
		OpGetTransactionReceipt:       {name: "GetTransactionReceipt"},
		OpGetMine:                     {name: "GetMine"},
		OpGenTx:                       {name: "GenTx"},
		OpGetTransactionListByAddress: {name: "GetTransactionListByAddress"},
		OpGetLogs:                     {name: "GetLogs"},
		OpEstimateGas:                 {name: "EstimateGas"},
		OpGetStorageAt:                {name: "GetStorageAt"},
		OpGetCode:                     {name: "GetCode"},
		OpGasPrice:                    {name: "GasPrice"},
		OpGetWork:                     {name: "GetWork"},
		OpSubmitWork:                  {name: "SubmitWork"},
		// p2p api
		OpGetMinorBlocks:       {name: "GetMinorBlocks"},
		OpGetMinorBlockHeaders: {name: "GetMinorBlockHeaders"},
		OpHandleNewTip:         {name: "HandleNewTip"},
		OpAddTransactions:      {name: "AddTransactions"},
	}
)

type opType struct {
	name string
}

type opNode struct {
	conn   *grpc.ClientConn
	client reflect.Value
}

// Client wraps the GRPC client.
type Client interface {
	Call(hostport string, req *Request) (*Response, error)
	GetOpName(uint32) string
}

type rpcClient struct {
	connVals map[string]*opNode
	funcs    map[uint32]opType

	mu      sync.RWMutex
	timeout time.Duration
	tp      serverType
	rpcId   int64
	logger  log.Logger
}

func (c *rpcClient) GetOpName(op uint32) string {
	return c.funcs[op].name
}

func (c *rpcClient) Call(hostport string, req *Request) (*Response, error) {
	_, ok := c.funcs[req.Op]
	if !ok {
		return nil, errors.New("invalid op")
	}
	req.RpcId = c.addRpcId()
	return c.grpcOp(hostport, req)
}

func (c *rpcClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, node := range c.connVals {
		node.conn.Close()
	}
}

func (c *rpcClient) getConn(hostport string) (*opNode, error) {
	// add new connection if not existing or has failed
	// note that race may happen when adding duplicate connections
	c.mu.Lock()
	defer c.mu.Unlock()
	node, ok := c.connVals[hostport]
	if !ok || node.conn.GetState() > connectivity.TransientFailure {
		return c.addConn(hostport)
	}

	return node, nil
}

func (c *rpcClient) grpcOp(hostport string, req *Request) (*Response, error) {

	node, err := c.getConn(hostport)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	var (
		val = []reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(req)}
		res *Response
	)

	rs := node.client.MethodByName(c.funcs[req.Op].name).Call(val)

	if !rs[1].IsNil() {
		err = rs[1].Interface().(error)
		return nil, err
	} else if !rs[0].IsNil() {
		res = rs[0].Interface().(*Response)
		return res, nil
	}
	panic(fmt.Sprintf("unforeseen event from %s, api %s", hostport, c.GetOpName(req.Op)))
}

func (c *rpcClient) addConn(hostport string) (*opNode, error) {

	delete(c.connVals, hostport)
	opts := []grpc.DialOption{grpc.WithInsecure()}
	conn, err := grpc.Dial(hostport, opts...)
	if err != nil {
		return nil, err
	}

	switch c.tp {
	case MasterServer:
		c.connVals[hostport] = &opNode{conn: conn, client: reflect.ValueOf(NewMasterServerSideOpClient(conn))}
	case SlaveServer:
		c.connVals[hostport] = &opNode{conn: conn, client: reflect.ValueOf(NewSlaveServerSideOpClient(conn))}
	}
	c.logger.Debug("Created new connection", "hostport", hostport)
	return c.connVals[hostport], nil
}

func (c *rpcClient) addRpcId() int64 {
	return atomic.AddInt64(&c.rpcId, 1)
}

// NewClient returns a new GRPC client wrapper.
func NewClient(serverType serverType) Client {
	rpcFuncs := masterApis
	if serverType == SlaveServer {
		rpcFuncs = slaveApis
	} else if serverType != MasterServer {
		return nil
	}
	return &rpcClient{
		connVals: make(map[string]*opNode),
		funcs:    rpcFuncs,
		tp:       serverType,
		timeout:  time.Duration(timeOut) * time.Second,
		logger:   log.New("rpcclient"),
	}
}