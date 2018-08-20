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
	"strconv"

	"github.com/dgraph-io/badger"
	"github.com/dispatchlabs/disgo/commons/services"
	"github.com/dispatchlabs/disgo/commons/types"
	"github.com/dispatchlabs/disgo/commons/utils"
	"github.com/dispatchlabs/disgo/commons/pubsub"
	"github.com/dispatchlabs/disgo/disgover"
)

// GetDelegateNodes
func (this *DAPoSService) GetDelegateNodes() *types.Response {

	// Find nodes.
	nodes, err := types.ToNodesByTypeFromCache(services.GetCache(), types.TypeDelegate)
	if err != nil {
		utils.Error(err)
		return types.NewResponseWithError(err)
	}

	// Create response.
	response := types.NewResponse()
	response.Data = nodes
	utils.Info("GetDelegateNodes")

	return response
}

// GetReceipt
func (this *DAPoSService) GetReceipt(transactionHash string) *types.Response {
	txn := services.NewTxn(false)
	defer txn.Discard()
	response := types.NewResponse()

	// Delegate?
	if disgover.GetDisGoverService().ThisNode.Type == types.TypeDelegate {
		receipt, err := types.ToReceiptFromCache(services.GetCache(), transactionHash)
		if err != nil {
			receipt, err = types.ToReceiptFromTransactionHash(txn, transactionHash)
			if err != nil {
				if err == badger.ErrKeyNotFound {
					response.Status = types.StatusNotFound
					response.HumanReadableStatus = fmt.Sprintf("unable to find receipt [hash=%s]", transactionHash)
				} else {
					response.Status = types.StatusInternalError
					response.HumanReadableStatus = err.Error()
				}
			} else {
				response.Data = receipt
			}
		} else {
			response.Data = receipt
		}
	} else {
		response.Status = types.StatusNotDelegate
		response.HumanReadableStatus = types.StatusNotDelegateAsHumanReadable
	}
	utils.Info(fmt.Sprintf("GetReceipt [hash=%s, status=%s]", transactionHash, response.Status))

	return response
}

// GetAccount
func (this *DAPoSService) GetAccount(address string) *types.Response {
	txn := services.NewTxn(true)
	defer txn.Discard()
	response := types.NewResponse()

	// Delegate?
	if disgover.GetDisGoverService().ThisNode.Type == types.TypeDelegate {
		account, err := types.ToAccountByAddress(txn, address)
		if err != nil {
			if err == badger.ErrKeyNotFound {
				response.Status = types.StatusNotFound
			} else {
				response.Status = types.StatusInternalError
			}
		} else {
			response.Data = account
			response.Status = types.StatusOk
		}
	} else {
		response.Status = types.StatusNotDelegate
		response.HumanReadableStatus = types.StatusNotDelegateAsHumanReadable
	}
	utils.Info(fmt.Sprintf("retrieved account [address=%s, status=%s]", address, response.Status))

	return response
}

// NewTransaction
func (this *DAPoSService) NewTransaction(transaction *types.Transaction) *types.Response {
	response := types.NewResponse()

	// Delegate?
	if disgover.GetDisGoverService().ThisNode.Type == types.TypeDelegate {
		response = this.startGossiping(transaction)
	} else {
		response.Status = types.StatusNotDelegate
		response.HumanReadableStatus = types.StatusNotDelegateAsHumanReadable
	}

	utils.Info(fmt.Sprintf("new transaction [hash=%s, status=%s]", transaction.Hash, response.Status))
	return response
}

// GetTransaction
func (this *DAPoSService) GetTransaction(hash string) *types.Response {
	txn := services.NewTxn(false)
	defer txn.Discard()
	response := types.NewResponse()

	// Delegate?
	if disgover.GetDisGoverService().ThisNode.Type == types.TypeDelegate {
		transaction, err := types.ToTransactionByHash(txn, hash)
		if err != nil {
			if err == badger.ErrKeyNotFound {
				response.Status = types.StatusNotFound
			} else {
				response.Status = types.StatusInternalError
			}
		} else {
			response.Data = transaction
			response.Status = types.StatusOk
		}
	} else {
		response.Status = types.StatusNotDelegate
		response.HumanReadableStatus = types.StatusNotDelegateAsHumanReadable
	}
	utils.Info(fmt.Sprintf("retrieved transaction [hash=%s, status=%s]", hash, response.Status))

	return response
}

// GetTransactions
func (this *DAPoSService) GetTransactionsOld() *types.Response { //TODO: to be depricated
	txn := services.NewTxn(true)
	defer txn.Discard()
	response := types.NewResponse()

	// Delegate?
	if disgover.GetDisGoverService().ThisNode.Type == types.TypeDelegate {
		var err error
		response.Data, err = types.ToTransactions(txn)
		if err != nil {
			response.Status = types.StatusInternalError
			response.HumanReadableStatus = err.Error()
		} else {
			response.Status = types.StatusOk
		}
	} else {
		response.Status = types.StatusNotDelegate
		response.HumanReadableStatus = types.StatusNotDelegateAsHumanReadable
	}

	utils.Info(fmt.Sprintf("retrieved transactions [status=%s]", response.Status))

	return response
}

// GetTransactions
func (this *DAPoSService) GetTransactions(page string) *types.Response {
	txn := services.NewTxn(true)
	defer txn.Discard()
	response := types.NewResponse()
	var err error
	pageNumber, err := strconv.Atoi(page)
	if err != nil {
		response.Status = types.StatusInternalError
		response.HumanReadableStatus = err.Error()
		return response
	}

	// Delegate?
	if disgover.GetDisGoverService().ThisNode.Type == types.TypeDelegate {

		response.Data, err = types.TransactionPaging(pageNumber, txn)
		if err != nil {
			response.Status = types.StatusInternalError
			response.HumanReadableStatus = err.Error()
		} else {
			response.Status = types.StatusOk
		}
	} else {
		response.Status = types.StatusNotDelegate
		response.HumanReadableStatus = types.StatusNotDelegateAsHumanReadable
	}

	utils.Info(fmt.Sprintf("GetTransactions [status=%s]", response.Status))

	return response
}

// GetTransactionsByFromAddress
func (this *DAPoSService) GetTransactionsByFromAddress(address string) *types.Response {
	txn := services.NewTxn(true)
	defer txn.Discard()
	response := types.NewResponse()

	// Delegate?
	if disgover.GetDisGoverService().ThisNode.Type == types.TypeDelegate {
		var err error
		response.Data, err = types.ToTransactionsByFromAddress(txn, address)
		if err != nil {
			response.Status = types.StatusInternalError
			response.HumanReadableStatus = err.Error()
		} else {
			response.Status = types.StatusOk
		}
	} else {
		response.Status = types.StatusNotDelegate
		response.HumanReadableStatus = types.StatusNotDelegateAsHumanReadable
	}

	utils.Info(fmt.Sprintf("retrieved transactions by from address [address=%s, status=%s]", address, response.Status))

	return response
}

// GetTransactionsByToAddress
func (this *DAPoSService) GetTransactionsByToAddress(address string) *types.Response {
	txn := services.NewTxn(true)
	defer txn.Discard()
	response := types.NewResponse()

	// Delegate?
	if disgover.GetDisGoverService().ThisNode.Type == types.TypeDelegate {
		var err error
		response.Data, err = types.ToTransactionsByToAddress(txn, address)
		if err != nil {
			response.Status = types.StatusInternalError
			response.HumanReadableStatus = err.Error()
		} else {
			response.Status = types.StatusOk
		}
	} else {
		response.Status = types.StatusNotDelegate
		response.HumanReadableStatus = types.StatusNotDelegateAsHumanReadable
	}

	utils.Info(fmt.Sprintf("retrieved transactions by to address [address=%s, status=%s]", address, response.Status))

	return response
}

func (this *DAPoSService) DumpQueue() *types.Response {
	response := types.NewResponse()
	response.Data = this.gossipQueue.Dump()
	return response
}

func (this *DAPoSService) ToBeSupported() *types.Response {
	response := types.NewResponse()
	response.Data = types.StatusUnavailableFeature
	return response
}

func (this *DAPoSService) GetAccounts(page string) *types.Response {
	txn := services.NewTxn(true)
	defer txn.Discard()
	response := types.NewResponse()
	var err error
	pageNumber, err := strconv.Atoi(page)
	if err != nil {
		response.Status = types.StatusInternalError
		response.HumanReadableStatus = err.Error()
		return response
	}

	// Delegate?
	if disgover.GetDisGoverService().ThisNode.Type == types.TypeDelegate {

		response.Data, err = types.AccountPaging(pageNumber, txn)
		if err != nil {
			response.Status = types.StatusInternalError
			response.HumanReadableStatus = err.Error()
		} else {
			response.Status = types.StatusOk
		}
	} else {
		response.Status = types.StatusNotDelegate
		response.HumanReadableStatus = types.StatusNotDelegateAsHumanReadable
	}

	utils.Info(fmt.Sprintf("GetAccounts [status=%s]", response.Status))

	return response
}

func (this *DAPoSService) GetGossips(page string) *types.Response {
	txn := services.NewTxn(true)
	defer txn.Discard()
	response := types.NewResponse()
	var err error
	pageNumber, err := strconv.Atoi(page)
	if err != nil {
		response.Status = types.StatusInternalError
		response.HumanReadableStatus = err.Error()
		return response
	}

	// Delegate?
	if disgover.GetDisGoverService().ThisNode.Type == types.TypeDelegate {

		response.Data, err = types.GossipPaging(pageNumber, txn)
		if err != nil {
			response.Status = types.StatusInternalError
			response.HumanReadableStatus = err.Error()
		} else {
			response.Status = types.StatusOk
		}
	} else {
		response.Status = types.StatusNotDelegate
		response.HumanReadableStatus = types.StatusNotDelegateAsHumanReadable
	}

	utils.Info(fmt.Sprintf("GetGossips [status=%s]", response.Status))

	return response
}

// GetAccount
func (this *DAPoSService) GetGossip(hash string) *types.Response {
	txn := services.NewTxn(true)
	defer txn.Discard()
	response := types.NewResponse()

	// Delegate?
	if disgover.GetDisGoverService().ThisNode.Type == types.TypeDelegate {
		gossip, err := types.ToGossipByTransactionHash(txn, hash)
		if err != nil {
			if err == badger.ErrKeyNotFound {
				response.Status = types.StatusNotFound
			} else {
				response.Status = types.StatusInternalError
			}
		} else {
			response.Data = gossip
			response.Status = types.StatusOk
		}
	} else {
		response.Status = types.StatusNotDelegate
		response.HumanReadableStatus = types.StatusNotDelegateAsHumanReadable
	}
	utils.Info(fmt.Sprintf("retrieved Gossip [tx hash=%s, status=%s]", hash, response.Status))

	return response
}

// CreateSubscription
func (this *DAPoSService) CreateSubscription(subReq pubsub.SubscriptionRequest) *types.Response {
	response := types.NewResponse()

	// Delegate?
	if disgover.GetDisGoverService().ThisNode.Type == types.TypeDelegate {
		T, err := this.HTTPPublisher.GetTopic(subReq.Topic)
		if err != nil {
			response.Status = types.StatusTopicNotFound
			response.HumanReadableStatus = fmt.Sprintf("Topic \"%s\" is invalid", subReq.Topic)
		} else {
			sub := pubsub.NewSubscription(subReq.Endpoint, subReq.Headers, subReq.Address)
			_, err = T.Subscribe(sub)
			if err != nil {
				response.Status = types.StatusTopicNotFound
				response.HumanReadableStatus = err.Error()
			} else {
				response.Data = fmt.Sprintf("{\"hash\":\"%s\"}", sub.Hash)
				response.Status = types.StatusOk
			}
		}
	} else {
		response.Status = types.StatusNotDelegate
		response.HumanReadableStatus = types.StatusNotDelegateAsHumanReadable
	}
	return response
}
