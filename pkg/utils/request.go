package utils

type Request struct {
	Resource  string      `json:"resource"`
	Action    string      `json:"action"`
	RequestId string      `json:"request_id"`
	Params    interface{} `json:"params"`
}
