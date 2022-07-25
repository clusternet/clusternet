package utils

import (
	"crypto/sha256"
	"encoding/base32"
	"strings"

	"k8s.io/client-go/tools/cache"
)

func HandleAllWith(f func(obj interface{})) cache.ResourceEventHandlerFuncs {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: f,
		UpdateFunc: func(old, new interface{}) {
			f(new)
		},
		DeleteFunc: func(obj interface{}) {
			tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
			if ok {
				f(tombstone.Obj)
			} else {
				f(obj)
			}
		},
	}
}

func DerivedName(name string) string {
	hash := sha256.New()
	hash.Write([]byte(name))
	return "derived-" + strings.ToLower(base32.HexEncoding.WithPadding(base32.NoPadding).EncodeToString(hash.Sum(nil)))[:10]
}
