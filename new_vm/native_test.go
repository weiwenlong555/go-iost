package new_vm

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/iost-official/Go-IOS-Protocol/core/contract"
	"github.com/iost-official/Go-IOS-Protocol/new_vm/database"
	"github.com/iost-official/Go-IOS-Protocol/new_vm/host"
	"github.com/iost-official/Go-IOS-Protocol/new_vm/native_vm"
)

var testDataPath = "./test_data/"

func MyInit(t *testing.T, conName string, optional ...interface{}) (*native_vm.VM, *host.Host, *contract.Contract) {
	db := database.NewDatabaseFromPath(testDataPath + conName + ".json")
	vi := database.NewVisitor(100, db)

	ctx := host.NewContext(nil)
	ctx.Set("gas_price", int64(1))
	var gasLimit = int64(10000)
	if len(optional) > 0 {
		gasLimit = optional[0].(int64)
	}
	ctx.GSet("gas_limit", gasLimit)
	ctx.Set("contract_name", conName)
	ctx.Set("tx_hash", []byte("iamhash"))

	pm := NewMonitor()
	h := host.NewHost(ctx, vi, pm, nil)

	code := &contract.Contract{
		ID: conName,
	}

	e := &native_vm.VM{}
	e.Init()

	return e, h, code
}

func ReadFile(src string) ([]byte, error) {
	fi, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer fi.Close()
	fd, err := ioutil.ReadAll(fi)
	if err != nil {
		return nil, err
	}
	return fd, nil
}

func TestEngine_SetCode(t *testing.T) {
	e, host, code := MyInit(t, "setcode")
	host.Ctx.Set("tx_hash", "iamhash")
	hash := "Contractiamhash"

	rawCode, err := ReadFile(testDataPath + "test.js")
	if err != nil {
		t.Fatalf("read file error: %v\n", err)
	}
	rawAbi, err := ReadFile(testDataPath + "test.js.abi")
	if err != nil {
		t.Fatalf("read file error: %v\n", err)
	}

	compiler := &contract.Compiler{}
	con, err := compiler.Parse("", string(rawCode), string(rawAbi))
	if err != nil {
		t.Fatalf("compiler parse error: %v\n", err)
	}

	rs, _, err := e.LoadAndCall(host, code, "SetCode", con.Encode())

	if err != nil {
		t.Fatalf("LoadAndCall setcode error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != hash {
		t.Errorf("LoadAndCall except Contract" + "iamhash" + ", got %s\n", rs[0])
	}

	con.ID = "Contractiamhash"
	rs, _, err = e.LoadAndCall(host, code, "DestroyCode", con.ID)
	if err == nil || err.Error() != "destroy refused" {
		t.Fatalf("LoadAndCall for should return destroy refused, but got %v\n", err)
	}

	rawCode, err = ReadFile(testDataPath + "test_new.js")
	if err != nil {
		t.Fatalf("read file error: %v\n", err)
	}
	rawAbi, err = ReadFile(testDataPath + "test_new.js.abi")
	if err != nil {
		t.Fatalf("read file error: %v\n", err)
	}
	con, err = compiler.Parse(con.ID, string(rawCode), string(rawAbi))
	if err != nil {
		t.Fatalf("compiler parse error: %v\n", err)
	}
	rs, _, err = e.LoadAndCall(host, code, "UpdateCode", con.Encode(), "")
	if err != nil {
		t.Fatalf("LoadAndCall update error: %v\n", err)
	}
	if len(rs) != 0 {
		t.Errorf("LoadAndCall except 0 rtn" + ", got %d\n", len(rs))
	}

	rs, _, err = e.LoadAndCall(host, code, "DestroyCode", con.ID)
	if err != nil {
		t.Fatalf("LoadAndCall destroy error: %v\n", err)
	}

	rs, _, err = e.LoadAndCall(host, code, "UpdateCode", con.Encode(), "")
	if err == nil || err.Error() != "contract not exists" {
		t.Fatalf("LoadAndCall for should return contract not exists, but got %v\n", err)
	}
}
