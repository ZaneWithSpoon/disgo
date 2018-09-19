/*
 *    This file is part of DAPoS library.
 *
 *    The DAPoS library is free software: you can redistribute it and/or modify
 *    it under the terms of the GNU General Public License as published by
 *    the Free Software Foundation, either version 3 of the License, or
 *    (at your option) any later version.
 *
 *    The DAPoS library is distributed in the hope that it will be useful,
 *    but WITHOUT ANY WARRANTY; without even the implied warranty of
 *    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *    GNU General Public License for more details.
 *
 *    You should have received a copy of the GNU General Public License
 *    along with the DAPoS library.  If not, see <http://www.gnu.org/licenses/>.
 */
package dapos

import (
	"fmt"
	"math/rand"

	"github.com/dgraph-io/badger"
	"github.com/dispatchlabs/disgo/commons/helper"
	"github.com/dispatchlabs/disgo/commons/services"
	"github.com/dispatchlabs/disgo/commons/types"
	"github.com/dispatchlabs/disgo/commons/utils"
	"github.com/dispatchlabs/disgo/disgover"

	"encoding/hex"
	"math/big"
	"strings"
	"time"

	"github.com/dispatchlabs/disgo/dvm"
	"github.com/dispatchlabs/disgo/dvm/ethereum/abi"
)

// startGossiping
func (this *DAPoSService) startGossiping(transaction *types.Transaction) *types.Response {
	txn := services.NewTxn(false)
	defer txn.Discard()

	// Verify?
	err := transaction.Verify()
	if err != nil {
		utils.Info(fmt.Sprintf("invalid transaction [hash=%s]", transaction.Hash))
		return types.NewResponseWithStatus(types.StatusInvalidTransaction, err.Error())
	}
	elapsedMilliSeconds := utils.ToMilliSeconds(time.Now()) - transaction.Time
	if elapsedMilliSeconds > types.TxReceiveTimeout {
		utils.Error(fmt.Sprintf("Timed out [hash=%s]", transaction.Hash))
		return types.NewResponseWithStatus(types.StatusTransactionTimeOut, "Transaction was received later than 3 second limit")
	}

	// Duplicate transaction?
	_, err = txn.Get([]byte(transaction.Key()))
	if err == nil {
		utils.Info(fmt.Sprintf("duplicate transaction [hash=%s]", transaction.Hash))
		return types.NewResponseWithStatus(types.StatusDuplicateTransaction, "Duplicate transaction")
	}
	if err != badger.ErrKeyNotFound {
		utils.Error(err)
		return types.NewResponseWithError(err)
	}

	// TODO: Check minimum hertz

	// Are we already gossiping about this transaction?
	_, err = types.ToTransactionFromCache(services.GetCache(), transaction.Hash)
	if err == nil {
		utils.Info(fmt.Sprintf("already processing this transaction [hash=%s]", transaction.Hash))
		return types.NewResponseWithStatus(types.StatusAlreadyProcessingTransaction, "Transaction is already being processed")
	}

	// Cache receipt.
	utils.Info("Cache receipt")
	receipt := types.NewReceipt(transaction.Hash)
	receipt.Cache(services.GetCache())

	// Cache gossip with my rumor.
	gossip := types.NewGossip(*transaction)
	rumor := types.NewRumor(types.GetAccount().PrivateKey, types.GetAccount().Address, transaction.Hash)
	gossip.Rumors = append(gossip.Rumors, *rumor)
	gossip.Cache(services.GetCache())

	// transaction.Receipt.Status = types.StatusReceived
	transaction.Cache(services.GetCache())

	this.gossipChan <- gossip

	return types.NewResponseWithStatus(types.StatusPending, "Pending")
}

// Temp_ProcessTransaction -
func (this *DAPoSService) Temp_ProcessTransaction(transaction *types.Transaction) *types.Response {
	// go func(tx *types.Transaction) {

	// Cache receipt.
	receipt := types.NewReceipt(transaction.Hash)
	receipt.Cache(services.GetCache())

	// Cache gossip with my rumor.
	gossip := types.NewGossip(*transaction)
	rumor := types.NewRumor(types.GetAccount().PrivateKey, types.GetAccount().Address, transaction.Hash)
	gossip.Rumors = append(gossip.Rumors, *rumor)
	gossip.Cache(services.GetCache())

	this.gossipChan <- gossip

	return types.NewResponseWithStatus(types.StatusPending, "Pending")
	// }(transaction)
}

// synchronizeGossip
func (this *DAPoSService) synchronizeGossip(gossip *types.Gossip) (*types.Gossip, error) {

	// Get or set receipt?
	_, err := types.ToReceiptFromCache(services.GetCache(), gossip.Transaction.Hash)
	if err != nil {
		receipt := types.NewReceipt(gossip.Transaction.Hash)
		receipt.Cache(services.GetCache())
	}

	// PersistAndCache synchronizedGossip.
	var synchronizedGossip *types.Gossip
	ourGossip, err := types.ToGossipFromCache(services.GetCache(), gossip.Transaction.Hash)
	if err != nil {
		synchronizedGossip = gossip
	} else {
		synchronizedGossip = ourGossip
		for _, rumor := range gossip.Rumors {
			if !synchronizedGossip.ContainsRumor(rumor.Address) && rumor.Verify() { // We don't want to propagate cryptographic lies.
				synchronizedGossip.Rumors = append(synchronizedGossip.Rumors, rumor)
			}
		}
	}

	// Did rumor?
	didRumor := false
	for _, rumor := range synchronizedGossip.Rumors {
		if rumor.Address == types.GetAccount().Address {
			didRumor = true
		}
	}
	if !didRumor {

		// We don't want to propagate cryptographic lies.
		err = gossip.Transaction.Verify()
		if err == nil {
			synchronizedGossip.Rumors = append(gossip.Rumors, *types.NewRumor(types.GetAccount().PrivateKey, types.GetAccount().Address, gossip.Transaction.Hash))
		} else {
			utils.Error(err)
			return synchronizedGossip, err
		}
	}
	return synchronizedGossip, nil
}

// gossipWorker
func (this *DAPoSService) gossipWorker() {
	var gossip *types.Gossip
	for {
		select {
		case gossip = <-this.gossipChan:

			go func(gossip *types.Gossip) {

				// Gossip timeout?
				if len(gossip.Rumors) > 1 {
					if !types.ValidateTimeDelta(gossip.Rumors) {
						utils.Warn("The rumors have an invalid time delta (greater than gossip timeout milliseconds")
						//ignore this gossip's rumors and hopefully still hit 2/3 from well timed gossip, but keep listening
						return
					}
				}

				// Find nodes in cache?
				delegateNodes, err := types.ToNodesByTypeFromCache(services.GetCache(), types.TypeDelegate)
				if err != nil {
					utils.Error(err)
					return
				}

				// Do we have 2/3 of rumors?
				if float32(len(gossip.Rumors)) >= float32(len(delegateNodes))*2/3 {
					if !this.gossipQueue.Exists(gossip.Transaction.Hash) {
						this.gossipQueue.Push(gossip)

						go func() {
							//adding timeout as a function of tx time.  If tx is in the future, add future delta to the default timeout
							delta := gossip.Transaction.Time - utils.ToMilliSeconds(time.Now())
							totalMilliseconds := (types.GossipTimeout * len(delegateNodes)) + types.TxReceiveTimeout
							timeout := time.Duration(totalMilliseconds)
							if delta > 0 {
								timeout = time.Millisecond*time.Duration(delta) + timeout
							}
							time.Sleep(timeout)
							this.timoutChan <- true
						}()
					}
				}

				// Did we already receive all the delegate's rumors?
				if len(gossip.Rumors) == len(delegateNodes) {
					utils.Debug("already received all rumors from delegates")
					return
				}

				// Get random delegate?
				node := this.getRandomDelegate(gossip, delegateNodes)
				if node == nil {
					utils.Warn("did not find any delegates to rumor with")
					this.gossipChan <- gossip
					return
				}

				// Peer gossip.
				peerGossip, err := this.peerGossipGrpc(*node, gossip)
				if err != nil {
					utils.Warn(err)
					this.gossipChan <- gossip
					return
				}
				this.gossipChan <- peerGossip
			}(gossip)
		}
	}
}

// getRandomDelegate
func (this *DAPoSService) getRandomDelegate(gossip *types.Gossip, delegateNodes []*types.Node) *types.Node {
	if len(delegateNodes) == 0 {
		return nil
	}

	// Get delegates that have not rumored?
	delegatesNotRumored := make([]*types.Node, 0)
	for _, node := range delegateNodes {
		if gossip.ContainsRumor(node.Address) || node.Address == disgover.GetDisGoverService().ThisNode.Address {
			continue
		}
		delegatesNotRumored = append(delegatesNotRumored, node)
	}
	if len(delegatesNotRumored) == 0 {
		return nil
	}

	// Find random delegate.
	rand.Seed(time.Now().UTC().UnixNano())
	index := rand.Intn(len(delegatesNotRumored))
	return delegatesNotRumored[index]
}

// gossipWorker - transfer tokens, deploy smart contract, and execution of smart contract.
func (this *DAPoSService) transactionWorker() {

	for {
		select {
		case <-this.timoutChan:
			this.doWork()
		}
	}
}

func (this *DAPoSService) doWork() {
	var gossip *types.Gossip

	if this.gossipQueue.HasAvailable() {
		gossip = this.gossipQueue.Pop()
		// Get receipt.
		receipt, err := types.ToReceiptFromCache(services.GetCache(), gossip.Transaction.Hash)
		if err != nil {
			utils.Error(fmt.Sprintf("receipt not found [hash=%s]", gossip.Transaction.Hash))
			receipt = types.NewReceipt(gossip.Transaction.Hash)
			receipt.Status = types.StatusReceiptNotFound
			receipt.Cache(services.GetCache())
			return
		}
		initialRcvDuration := gossip.Rumors[0].Time - gossip.Transaction.Time
		utils.Info("Initial Receive Duration = ", initialRcvDuration, types.TxReceiveTimeout)
		if initialRcvDuration >= types.TxReceiveTimeout {
			utils.Error(fmt.Sprintf("Timed out [hash=%s] %v milliseconds", gossip.Transaction.Hash, initialRcvDuration))
			receipt = types.NewReceipt(gossip.Transaction.Hash)
			receipt.Status = types.StatusTransactionTimeOut
			receipt.Cache(services.GetCache())
			return
		}
		receipt.Created = time.Now()
		if types.GetConfig().IsBookkeeper {
			executeTransaction(&gossip.Transaction, receipt, gossip)
		}
	}
}

// executeTransaction
func executeTransaction(transaction *types.Transaction, receipt *types.Receipt, gossip *types.Gossip) {
	utils.Debug("executeTransaction --> ", transaction.Hash)
	services.Lock(transaction.Hash)
	defer services.Unlock(transaction.Hash)
	txn := services.NewTxn(true)
	defer txn.Discard()

	utils.Debug("executing transaction")
	// Has this transaction already been processed?
	_, err := txn.Get([]byte(transaction.Key()))
	if err == nil {
		return
	}

	// Find/create fromAccount?
	now := time.Now()
	fromAccount, err := types.ToAccountByAddress(txn, transaction.From)
	if err != nil {
		if err == badger.ErrKeyNotFound {
			fromAccount = &types.Account{Address: transaction.From, Balance: big.NewInt(0), Created: now}
		} else {
			utils.Error(err)
			receipt.Status = types.StatusInternalError
			receipt.HumanReadableStatus = err.Error()
			receipt.Cache(services.GetCache())
			return
		}
	}

	// Find/create toAccount?
	toAccount, err := types.ToAccountByAddress(txn, transaction.To)
	if err != nil {
		if err == badger.ErrKeyNotFound {
			toAccount = &types.Account{Address: transaction.To, Balance: big.NewInt(0), Created: now}
		} else {
			utils.Error(err)
			receipt.SetInternalErrorWithNewTransaction(services.GetDb(), err)
			return
		}
	}

	// Execute.
	switch transaction.Type {
	case types.TypeTransferTokens:

		// Sufficient tokens?
		if fromAccount.Balance.Int64() < transaction.Value {
			utils.Error(fmt.Sprintf("insufficient tokens [hash=%s]", transaction.Hash))
			receipt.SetStatusWithNewTransaction(services.GetDb(), types.StatusInsufficientTokens)
			return
		}
		fromAccount.Balance.SetInt64(fromAccount.Balance.Int64() - transaction.Value)
		toAccount.Balance.SetInt64(toAccount.Balance.Int64() + transaction.Value)
		utils.Info(fmt.Sprintf("transferred tokens [hash=%s, rumors=%d]", transaction.Hash, len(gossip.Rumors)))
		break
	case types.TypeDeploySmartContract:
		dvmService := dvm.GetDVMService()

		// ENCODE to HEX here, the DECODE is happening in GetABI()
		transaction.Abi = hex.EncodeToString([]byte(transaction.Abi))

		dvmResult, err := dvmService.DeploySmartContract(transaction)
		if err != nil {
			utils.Error(err, utils.GetCallStackWithFileAndLineNumber())
			receipt.Status = types.StatusInternalError
			receipt.HumanReadableStatus = err.Error()
			receipt.Cache(services.GetCache())
			return
		}

		err = processDVMResult(transaction, dvmResult, receipt)
		if err != nil {
			utils.Error(err)
			receipt.Status = types.StatusInternalError
			receipt.HumanReadableStatus = err.Error()
			receipt.Cache(services.GetCache())
			return
		}

		// Persist contract account.
		contractAccount := &types.Account{Address: hex.EncodeToString(dvmResult.ContractAddress[:]), Balance: big.NewInt(0), TransactionHash: transaction.Hash, Updated: now, Created: now}
		err = contractAccount.Persist(txn)
		if err != nil {
			utils.Error(err)
			receipt.Status = types.StatusInternalError
			receipt.HumanReadableStatus = err.Error()
			receipt.Cache(services.GetCache())
			return
		}
		receipt.ContractAddress = contractAccount.Address
		utils.Info(fmt.Sprintf("deployed contract [hash=%s, contractAddress=%s]", transaction.Hash, contractAccount.Address))
		break
	case types.TypeExecuteSmartContract:

		// READ PARAMS
		// if transaction.Type == types.TypeExecuteSmartContract {
		contractTx, err := types.ToTransactionByAddress(txn, transaction.To)
		if err != nil {
			utils.Error(err, utils.GetCallStackWithFileAndLineNumber())
			receipt.Status = types.StatusInternalError
			receipt.HumanReadableStatus = err.Error()
			receipt.Cache(services.GetCache())
			return
		}

		transaction.Abi = contractTx.Abi
		transaction.Params, err = helper.GetConvertedParams(transaction)
		if err != nil {
			utils.Error(err, utils.GetCallStackWithFileAndLineNumber())
			receipt.Status = types.StatusInternalError
			receipt.HumanReadableStatus = err.Error()
			receipt.Cache(services.GetCache())
			return
		}
		// }

		dvmService := dvm.GetDVMService()
		dvmResult, err1 := dvmService.ExecuteSmartContract(transaction)
		if err1 != nil {
			utils.Error(err, utils.GetCallStackWithFileAndLineNumber())
		}

		err = processDVMResult(transaction, dvmResult, receipt)
		if err != nil {
			utils.Error(err)
			receipt.Status = types.StatusInternalError
			receipt.HumanReadableStatus = err.Error()
			receipt.Cache(services.GetCache())
			return
		}
		receipt.ContractAddress = transaction.To
		utils.Info(fmt.Sprintf("executed contract [hash=%s, contractAddress=%s]", transaction.Hash, transaction.To))
		break
	default:
		utils.Error(fmt.Sprintf("invalid transaction type [hash=%s]", transaction.Hash))
		receipt.SetStatusWithNewTransaction(services.GetDb(), types.StatusInvalidTransaction)
		return
	}

	// Persist transaction
	err = transaction.Persist(txn)
	if err != nil {
		utils.Error(err)
		receipt.Status = types.StatusInternalError
		receipt.HumanReadableStatus = err.Error()
		receipt.Cache(services.GetCache())
		return
	}

	// Save fromAccount.
	fromAccount.Updated = now
	err = fromAccount.Persist(txn)
	if err != nil {
		utils.Error(err)
		receipt.Status = types.StatusInternalError
		receipt.HumanReadableStatus = err.Error()
		receipt.Cache(services.GetCache())
		return
	}

	// Save toAccount.
	toAccount.Updated = now
	err = toAccount.Persist(txn)
	if err != nil {
		utils.Error(err)
		receipt.Status = types.StatusInternalError
		receipt.HumanReadableStatus = err.Error()
		receipt.Cache(services.GetCache())
		return
	}

	// Save receipt.
	receipt.Status = types.StatusOk
	err = receipt.Set(txn, services.GetCache())
	if err != nil {
		utils.Error(err)
		receipt.Status = types.StatusInternalError
		receipt.HumanReadableStatus = err.Error()
		receipt.Cache(services.GetCache())
		return
	}

	// Save gossip.
	err = gossip.Set(txn, services.GetCache())
	if err != nil {
		utils.Error(err)
		receipt.Status = types.StatusInternalError
		receipt.HumanReadableStatus = err.Error()
		receipt.Cache(services.GetCache())
		return
	}

	// Commit.
	err = txn.Commit(nil)
	if err != nil {
		if err == badger.ErrConflict { // Another thread already committed this transaction. This will happen, which is ok.
			return
		}
		utils.Error(err)
		receipt.Status = types.StatusInternalError
		receipt.HumanReadableStatus = err.Error()
		receipt.Cache(services.GetCache())
		return
	}
}

//TODO: implement if useful
//func commit(transaction *types.Transaction) {}
// processDVMResult
func processDVMResult(transaction *types.Transaction, dvmResult *dvm.DVMResult, receipt *types.Receipt) error {
	utils.Info("######### DUMPING-DVMResult #########")
	utils.Info(dvmResult)

	if dvmResult.ContractMethodExecError != nil {
		utils.Error(dvmResult.ContractMethodExecError)
		return dvmResult.ContractMethodExecError
	}

	var errorToReturn error

	// Try read the execution result
	if len(strings.TrimSpace(dvmResult.ABI)) > 0 {
		fromHexAsByteArray, _ := hex.DecodeString(dvmResult.ABI)
		abiAsString := string(fromHexAsByteArray)
		jsonABI, err := abi.JSON(strings.NewReader(abiAsString))
		if err == nil {

			if method, ok := jsonABI.Methods[dvmResult.ContractMethod]; ok {
				if dvmResult.ContractMethodExecResult != nil && len(dvmResult.ContractMethodExecResult) > 0 {
					marshalledValues, err := method.Outputs.UnpackValues(dvmResult.ContractMethodExecResult)
					if err == nil {
						utils.Info(fmt.Sprintf("CONTRACT-CALL-RES: %v", marshalledValues))
						receipt.ContractResult = marshalledValues
					} else {
						errorToReturn = err
						utils.Error(err)
					}
				}
			}

			// var parsedRes []interface{}
			// var parsedRes = make([]interface{}, 3)
			// err = jsonABI.Unpack(&parsedRes, transaction.Method, dvmResult.ContractMethodExecResult)
			// if err == nil {
			// 	utils.Info(fmt.Sprintf("CONTRACT-CALL-RES: %s", parsedRes))
			// 	receipt.ContractResult = parsedRes
			// } else {
			// 	errorToReturn = err
			// 	utils.Error(err)
			// }
		} else {
			errorToReturn = err
			utils.Error(err)
		}
	}

	return errorToReturn
}
