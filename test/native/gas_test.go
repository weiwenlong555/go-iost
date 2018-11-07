package native

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/iost-official/go-iost/account"
	"github.com/iost-official/go-iost/common"
	"github.com/iost-official/go-iost/core/contract"
	"github.com/iost-official/go-iost/crypto"
	"github.com/iost-official/go-iost/db"
	"github.com/iost-official/go-iost/ilog"
	"github.com/iost-official/go-iost/vm"
	"github.com/iost-official/go-iost/vm/database"
	"github.com/iost-official/go-iost/vm/host"
	"github.com/iost-official/go-iost/vm/native"
)

func toString(n int64) string {
	return strconv.FormatInt(n, 10)
}

func toIOSTFN(n int64) *common.Fixed {
	return &common.Fixed{Value: n * native.IOSTRatio, Decimal: 8}
}

const initCoin int64 = 5000

var initCoinFN = toIOSTFN(initCoin)

const initNumber int64 = 10

func gasTestInit() (*native.Impl, *host.Host, *contract.Contract, *account.Account) {
	var tmpDB db.MVCCDB
	tmpDB, err := db.NewMVCCDB("mvcc")
	defer func() {
		tmpDB.Close()
		os.RemoveAll("mvcc")
	}()
	visitor := database.NewVisitor(100, tmpDB)
	if err != nil {
		panic(err)
	}
	context := host.NewContext(nil)
	context.Set("gas_price", int64(1))
	context.GSet("gas_limit", int64(100000))

	monitor := vm.NewMonitor()
	h := host.NewHost(context, visitor, monitor, nil)
	testAcc := getTestAccount()
	as, err := json.Marshal(testAcc)
	if err != nil {
		panic(err)
	}
	h.DB().MPut("iost.auth-account", testAcc.ID, database.MustMarshal(string(as)))
	h.Context().Set("number", initNumber)
	h.Context().Set("stack_height", 0)
	h.Context().Set("contract_name", "iost.token")
	h.Context().Set("publisher", testAcc.ID)

	tokenContract := native.TokenABI()
	h.SetCode(tokenContract)

	authList := make(map[string]int)
	h.Context().Set("auth_contract_list", authList)
	authList[testAcc.ID] = 2
	h.Context().Set("auth_list", authList)

	code := &contract.Contract{
		ID: "iost.gas",
	}

	e := &native.Impl{}
	e.Init()

	_, _, err = e.LoadAndCall(h, tokenContract, "create", "iost", testAcc.ID, int64(initCoin), []byte("{}"))
	if err != nil {
		panic("create iost " + err.Error())
	}
	_, _, err = e.LoadAndCall(h, tokenContract, "issue", "iost", testAcc.ID, fmt.Sprintf("%d", initCoin))
	if err != nil {
		panic("issue iost " + err.Error())
	}
	if initCoin*1e8 != visitor.TokenBalance("iost", testAcc.ID) {
		panic("set initial coins failed " + strconv.FormatInt(visitor.TokenBalance("iost", testAcc.ID), 10))
	}
	h.Context().Set("contract_name", "iost.gas")

	return e, h, code, testAcc
}

func getAccount(k string) *account.Account {
	key, err := account.NewKeyPair(common.Base58Decode(k), crypto.Ed25519)
	if err != nil {
		panic(err)
	}
	a := account.NewInitAccount(key.ID, key.ID, key.ID)
	return a
}

func getTestAccount() *account.Account {
	return getAccount("4nXuDJdU9MfP1TBY1W75o6ePDZNFuQ563YdkqVeEjW92aBcE6QDtFKPFWRBeKP8uMZcP7MGjfGubCLtu75t4ntxD")
}

func TestGas_NoPledge(t *testing.T) {
	ilog.Info("test an account who did not pledge has 0 gas")
	_, h, _, testAcc := gasTestInit()
	gas := h.GasManager.CurrentGas(testAcc.ID)
	if gas.Value != 0 {
		t.Fatalf("initial gas error %d", gas)
	}
}

func TestGas_PledgeAuth(t *testing.T) {
	ilog.Info("test pledging requires auth")
	e, h, code, testAcc := gasTestInit()
	pledgeAmount := toIOSTFN(200)
	authList := make(map[string]int)
	h.Context().Set("auth_list", authList)
	_, _, err := e.LoadAndCall(h, code, "PledgeGas", testAcc.ID, pledgeAmount.ToString())
	if err == nil {
		t.Fatalf("checking auth should not succeed")
	}
}

func TestGas_NotEnoughMoney(t *testing.T) {
	ilog.Info("test pledging with not enough money")
	e, h, code, testAcc := gasTestInit()
	pledgeAmount := toIOSTFN(20000)
	_, _, err := e.LoadAndCall(h, code, "PledgeGas", testAcc.ID, pledgeAmount.ToString())
	if err == nil {
		t.Fatalf("pledging with not enough money should not succeed")
	}
}

func TestGas_Pledge(t *testing.T) {
	ilog.Info("test pledge")
	e, h, code, testAcc := gasTestInit()
	pledgeAmount := toIOSTFN(200)
	_, _, err := e.LoadAndCall(h, code, "PledgeGas", testAcc.ID, pledgeAmount.ToString())
	if err != nil {
		t.Fatalf("pledge err %v", err)
	}
	if h.DB().TokenBalance("iost", testAcc.ID) != (initCoinFN.Value - pledgeAmount.Value) {
		t.Fatalf("invalid balance after pledge %d", h.DB().TokenBalance("iost", testAcc.ID))
	}
	if h.DB().TokenBalance("iost", "iost.gas") != pledgeAmount.Value {
		t.Fatalf("invalid balance after pledge %d", h.DB().TokenBalance("iost", host.ContractAccountPrefix+"iost.gas"))
	}
	ilog.Info("After pledge, you will get some gas immediately")
	gas := h.GasManager.CurrentGas(testAcc.ID)
	gasEstimated := pledgeAmount.Multiply(native.GasImmediateReward)
	if !gas.Equals(gasEstimated) {
		t.Fatalf("invalid gas %d != %d", gas, gasEstimated)
	}
	ilog.Info("Then gas increases at a predefined rate")
	delta := int64(5)
	h.Context().Set("number", initNumber+delta)
	gas = h.GasManager.CurrentGas(testAcc.ID)
	gasEstimated = pledgeAmount.Multiply(native.GasImmediateReward).Add(pledgeAmount.Multiply(native.GasIncreaseRate).Times(delta))
	if !gas.Equals(gasEstimated) {
		t.Fatalf("invalid gas %d != %d", gas, gasEstimated)
	}
	ilog.Info("Then gas will reach limit and not increase any longer")
	delta = int64(100)
	h.Context().Set("number", initNumber+delta)
	gas = h.GasManager.CurrentGas(testAcc.ID)
	gasEstimated = pledgeAmount.Multiply(native.GasLimit)
	if !gas.Equals(gasEstimated) {
		t.Fatalf("invalid gas %d != %d", gas, gasEstimated)
	}
}

func TestGas_PledgeForOther(t *testing.T) {
	ilog.Info("test pledge for others")
	e, h, code, testAcc := gasTestInit()
	otherAcc := getAccount("5oyBNyBeMFUKndGF8E3xkxmS3qugdYbwntSu8NEYtvC2DMmVcXgtmBqRxCLUCjxcu9zdcH3RkfKec3Q2xeiG48RL")
	pledgeAmount := toIOSTFN(200)
	_, _, err := e.LoadAndCall(h, code, "PledgeGas", otherAcc.ID, pledgeAmount.ToString())
	if err != nil {
		t.Fatalf("pledge err %v", err)
	}
	if h.DB().TokenBalance("iost", testAcc.ID) != (initCoinFN.Value - pledgeAmount.Value) {
		t.Fatalf("invalid balance after pledge %d", h.DB().TokenBalance("iost", testAcc.ID))
	}
	if h.DB().TokenBalance("iost", "iost.gas") != pledgeAmount.Value {
		t.Fatalf("invalid balance after pledge %d", h.DB().TokenBalance("iost", host.ContractAccountPrefix+"iost.gas"))
	}
	ilog.Info("After pledge, you will get some gas immediately")
	gas := h.GasManager.CurrentGas(otherAcc.ID)
	gasEstimated := pledgeAmount.Multiply(native.GasImmediateReward)
	if !gas.Equals(gasEstimated) {
		t.Fatalf("invalid gas %d != %d", gas, gasEstimated)
	}
	ilog.Info("If one pledge for others, he will get no gas himself")
	gas = h.GasManager.CurrentGas(testAcc.ID)
	if gas.Value != 0 {
		t.Fatalf("invalid gas should be empty buy get %v", gas)
	}

}

func TestGas_PledgeMore(t *testing.T) {
	ilog.Info("test you can pledge more after first time pledge")
	e, h, code, testAcc := gasTestInit()
	firstTimePledgeAmount := toIOSTFN(200)
	_, _, err := e.LoadAndCall(h, code, "PledgeGas", testAcc.ID, firstTimePledgeAmount.ToString())
	if err != nil {
		t.Fatalf("pledge err %v", err)
	}
	delta1 := int64(5)
	h.Context().Set("number", initNumber+delta1)
	gasBeforeSecondPledge := h.GasManager.CurrentGas(testAcc.ID)
	secondTimePledgeAmount := toIOSTFN(300)
	_, _, err = e.LoadAndCall(h, code, "PledgeGas", testAcc.ID, secondTimePledgeAmount.ToString())
	if err != nil {
		t.Fatalf("pledge err %v", err)
	}
	delta2 := int64(10)
	h.Context().Set("number", initNumber+delta1+delta2)
	gasAfterSecondPledge := h.GasManager.CurrentGas(testAcc.ID)
	gasEstimated := gasBeforeSecondPledge.Add(secondTimePledgeAmount.Multiply(native.GasImmediateReward).Add(
		secondTimePledgeAmount.Add(firstTimePledgeAmount).Multiply(native.GasIncreaseRate).Times(delta2)))
	if !gasAfterSecondPledge.Equals(gasEstimated) {
		t.Fatalf("invalid gas %d != %d", gasAfterSecondPledge, gasEstimated)
	}
	if h.DB().TokenBalance("iost", testAcc.ID) != initCoinFN.Sub(firstTimePledgeAmount).Sub(secondTimePledgeAmount).Value {
		t.Fatalf("invalid balance after pledge %d", h.DB().TokenBalance("iost", testAcc.ID))
	}
	if h.DB().TokenBalance("iost", "iost.gas") != firstTimePledgeAmount.Add(secondTimePledgeAmount).Value {
		t.Fatalf("invalid balance after pledge %d", h.DB().TokenBalance("iost", host.ContractAccountPrefix+"iost.gas"))
	}
}

func TestGas_UseGas(t *testing.T) {
	ilog.Info("test using gas")
	e, h, code, testAcc := gasTestInit()
	pledgeAmount := int64(200)
	_, _, err := e.LoadAndCall(h, code, "PledgeGas", testAcc.ID, toString(pledgeAmount))
	if err != nil {
		t.Fatalf("pledge err %v", err)
	}
	delta1 := int64(5)
	h.Context().Set("number", initNumber+delta1)
	gasBeforeUse := h.GasManager.CurrentGas(testAcc.ID)
	gasCost := toIOSTFN(100)
	err = h.GasManager.CostGas(testAcc.ID, gasCost)
	if err != nil {
		t.Fatalf("cost gas failed %v", err)
	}
	gasAfterUse := h.GasManager.CurrentGas(testAcc.ID)
	gasEstimated := gasBeforeUse.Sub(gasCost)
	if !gasAfterUse.Equals(gasEstimated) {
		t.Fatalf("invalid gas %d != %d", gasAfterUse, gasEstimated)
	}
}

func TestGas_Unpledge(t *testing.T) {
	ilog.Info("test unpledge")
	e, h, code, testAcc := gasTestInit()
	pledgeAmount := toIOSTFN(200)
	_, _, err := e.LoadAndCall(h, code, "PledgeGas", testAcc.ID, pledgeAmount.ToString())
	if err != nil {
		t.Fatalf("pledge err %v", err)
	}
	delta1 := int64(5)
	h.Context().Set("number", initNumber+delta1)
	unpledgeAmount := toIOSTFN(100)
	_, _, err = e.LoadAndCall(h, code, "UnpledgeGas", testAcc.ID, unpledgeAmount.ToString())
	if err != nil {
		t.Fatalf("unpledge err %v", err)
	}
	if h.DB().TokenBalance("iost", testAcc.ID) != initCoinFN.Sub(pledgeAmount).Add(unpledgeAmount).Value {
		t.Fatalf("invalid balance after unpledge %d", h.DB().TokenBalance("iost", testAcc.ID))
	}
	if h.DB().TokenBalance("iost", "iost.gas") != pledgeAmount.Sub(unpledgeAmount).Value {
		t.Fatalf("invalid balance after unpledge %d", h.DB().TokenBalance("iost", host.ContractAccountPrefix+"iost.gas"))
	}
	gas := h.GasManager.CurrentGas(testAcc.ID)
	ilog.Info("After unpledging, the gas limit will decrease. If current gas is more than the new limit, it will be decrease.")
	gasEstimated := pledgeAmount.Sub(unpledgeAmount).Multiply(native.GasLimit)
	if !gas.Equals(gasEstimated) {
		t.Fatalf("invalid gas %d != %d", gas, gasEstimated)
	}
}

func TestGas_UnpledgeTooMuch(t *testing.T) {
	ilog.Info("test unpledge too much: each account has a minimum pledge")
	e, h, code, testAcc := gasTestInit()
	pledgeAmount := int64(200)
	_, _, err := e.LoadAndCall(h, code, "PledgeGas", testAcc.ID, toString(pledgeAmount))
	if err != nil {
		t.Fatalf("pledge err %v", err)
	}
	delta1 := int64(1)
	h.Context().Set("number", initNumber+delta1)
	unpledgeAmount := (pledgeAmount - native.GasMinPledgeInIOST) + int64(1)
	_, _, err = e.LoadAndCall(h, code, "UnpledgeGas", testAcc.ID, toString(unpledgeAmount))
	if err == nil {
		t.Fatalf("unpledge should fail %v", err)
	}
}
