package master

import (
	"bytes"
	"errors"
	"github.com/QuarkChain/goquarkchain/account"
	"github.com/QuarkChain/goquarkchain/cluster/rpc"
	"github.com/QuarkChain/goquarkchain/consensus"
	"github.com/QuarkChain/goquarkchain/core/types"
	"github.com/QuarkChain/goquarkchain/serialize"
	"github.com/ethereum/go-ethereum/common"
)

func (s *MasterBackend) AddTransaction(tx *types.Transaction) error {
	evmTx := tx.EvmTx
	//TODO :SetQKCConfig
	branch := account.Branch{Value: evmTx.FromFullShardId()}
	slaves, ok := s.branchToSlaves[branch.Value]
	if !ok {
		return errors.New("no such slave")
	}
	lenSlaves := len(slaves)
	check := NewCheckErr(lenSlaves)
	for index := range slaves {
		check.wg.Add(1)
		go func(slave *SlaveConnection) {
			defer check.wg.Done()
			err := slave.AddTransaction(tx) //TODO ??height
			check.errc <- err

		}(slaves[index])
	}
	check.wg.Wait()
	if err := check.check(); err != nil {
		return err
	}

	return nil //TODO?? peer broadcast
}

func (s *MasterBackend) ExecuteTransaction(tx *types.Transaction, address account.Address, height *uint64) ([]byte, error) {
	evmTx := tx.EvmTx
	//TODO setQuarkChain
	branch := account.Branch{Value: evmTx.FromFullShardId()}

	slaves, ok := s.branchToSlaves[branch.Value]
	if !ok {
		return nil, errors.New("no such slave")
	}
	lenSlaves := len(slaves)
	check := NewCheckErr(lenSlaves)
	chanRsp := make(chan []byte, lenSlaves)
	for index := range slaves {
		check.wg.Add(1)
		go func(slave *SlaveConnection) {
			defer check.wg.Done()
			rsp, err := slave.ExecuteTransaction(tx, address, height) //TODO ??height
			check.errc <- err
			chanRsp <- rsp

		}(slaves[index])
	}
	check.wg.Wait()
	close(chanRsp)
	if err := check.check(); err != nil {
		return nil, err
	}

	flag := true
	firstFlag := 1
	var onlyValue []byte
	for res := range chanRsp {
		if firstFlag == 1 {
			firstFlag = 0
			onlyValue = res
		}
		if res == nil || !bytes.Equal(res, onlyValue) {
			flag = false
			break
		}
	}
	if flag == false {
		return nil, errors.New("flag==false")
	}
	return onlyValue, nil
}
func (s *MasterBackend) GetMinorBlockByHash(blockHash common.Hash, branch account.Branch) (*types.MinorBlock, error) {
	slaveConn, err := s.getSlaveConnection(branch)
	if err != nil {
		return nil, err
	}
	return slaveConn.GetMinorBlockByHash(blockHash, branch)
}
func (s *MasterBackend) GetMinorBlockByHeight(height uint64, branch account.Branch) (*types.MinorBlock, error) {
	slaveConn, err := s.getSlaveConnection(branch)
	if err != nil {
		return nil, err
	}
	return slaveConn.GetMinorBlockByHeight(height, branch)
}
func (s *MasterBackend) GetTransactionByHash(txHash common.Hash, branch account.Branch) (*types.MinorBlock, uint32, error) {
	slaveConn, err := s.getSlaveConnection(branch)
	if err != nil {
		return nil, 0, err
	}
	return slaveConn.GetTransactionByHash(txHash, branch)
}
func (s *MasterBackend) GetTransactionReceipt(txHash common.Hash, branch account.Branch) (*types.MinorBlock, uint32, *types.Receipt, error) {
	slaveConn, err := s.getSlaveConnection(branch)
	if err != nil {
		return nil, 0, nil, err
	}
	return slaveConn.GetTransactionReceipt(txHash, branch)
}

func (s *MasterBackend) GetTransactionsByAddress(address account.Address, start []byte, limit uint32) ([]*rpc.TransactionDetail, []byte, error) {
	fullShardID := s.clusterConfig.Quarkchain.GetFullShardIdByFullShardKey(address.FullShardKey)
	slaveConn, err := s.getSlaveConnection(account.Branch{Value: fullShardID})
	if err != nil {
		return nil, nil, err
	}
	return slaveConn.GetTransactionsByAddress(address, start, limit)
}
func (s *MasterBackend) GetLogs(branch account.Branch, address []account.Address, topics []*rpc.Topic, startBlock, endBlock *rpc.BlockHeight) ([]*types.Log, error) {
	slaveConn, err := s.getSlaveConnection(branch)
	if err != nil {
		return nil, err
	}

	if startBlock.Str == "latest" {
		startBlock.Height = s.branchToShardStats[branch.Value].Height
	}
	if endBlock.Str == "latest" {
		endBlock.Height = s.branchToShardStats[branch.Value].Height
	}
	return slaveConn.GetLogs(branch, address, topics, startBlock.Height, endBlock.Height)
}

func (s *MasterBackend) EstimateGas(tx *types.Transaction, fromAddress account.Address) (uint32, error) {
	evmTx := tx.EvmTx
	//TODO set config
	slaveConn, err := s.getSlaveConnection(account.Branch{Value: evmTx.FromFullShardId()})
	if err != nil {
		return 0, err
	}
	return slaveConn.EstimateGas(tx, fromAddress)
}
func (s *MasterBackend) GetStorageAt(address account.Address, key *serialize.Uint256, height uint64) (*serialize.Uint256, error) {
	fullShardID := s.clusterConfig.Quarkchain.GetFullShardIdByFullShardKey(address.FullShardKey)
	slaveConn, err := s.getSlaveConnection(account.Branch{Value: fullShardID})
	if err != nil {
		return nil, err
	}
	return slaveConn.GetStorageAt(address, key, height)
}

func (s *MasterBackend) GetCode(address account.Address, height uint64) ([]byte, error) {
	fullShardID := s.clusterConfig.Quarkchain.GetFullShardIdByFullShardKey(address.FullShardKey)
	slaveConn, err := s.getSlaveConnection(account.Branch{Value: fullShardID})
	if err != nil {
		return nil, err
	}
	return slaveConn.GetCode(address, height)
}

func (s *MasterBackend) GasPrice(branch account.Branch) (uint64, error) {
	slaveConn, err := s.getSlaveConnection(branch)
	if err != nil {
		return 0, err
	}
	return slaveConn.GasPrice(branch)
}
func (s *MasterBackend) GetWork(branch account.Branch) consensus.MiningWork {
	panic("not ")
}
func (s *MasterBackend) SubmitWork(branch account.Branch, headerHash common.Hash, nonce uint64, mixHash common.Hash) bool {
	return false
}
