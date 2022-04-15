package utils

type Void struct{}

var Ok Void

func Contains(strList []string, str string) bool {
	for _, s := range strList {
		if s == str {
			return true
		}
	}
	return false
}

func int32Ptr(i int32) *int32 { return &i }

func StringPtr(s string) *string { return &s }
