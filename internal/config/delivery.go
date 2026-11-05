package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/slapshot/gorelay/internal/delivery"
)

type destinationFile struct {
	Destinations []struct {
		ID string `json:"id"`
		URL string `json:"url"`
		SecretEnv string `json:"secret_env"`
		AllowLoopback bool `json:"allow_loopback"`
	} `json:"destinations"`
}

// loadDeliveryTargets refuses symlinks, broadly accessible configuration files,
// missing keys and missing authorized endpoints before the HTTP service starts.
func loadDeliveryTargets(path string)([]delivery.Target,error){
	if !filepath.IsAbs(path){return nil,errors.New("GORELAY_DESTINATIONS_FILE must be absolute")}
	info,err:=os.Lstat(path)
	if err!=nil{return nil,fmt.Errorf("could not stat destination config: %w",err)}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077!=0 {
		return nil,errors.New("destination config must be a private regular file")
	}
	data,err:=os.ReadFile(path)
	if err!=nil{return nil,fmt.Errorf("could not read destination config: %w",err)}
	if len(data)>65536{return nil,errors.New("destination config exceeds limit")}
	var parsed destinationFile
	if err:=json.Unmarshal(data,&parsed);err!=nil{return nil,errors.New("invalid destination configuration")}
	if len(parsed.Destinations)==0{return nil,errors.New("no destinations configured")}
	targets:=make([]delivery.Target,0,len(parsed.Destinations))
	seen:=map[string]bool{}
	for _,d:=range parsed.Destinations{
		if d.ID=="" || seen[d.ID] || !strings.HasPrefix(d.SecretEnv,"GORELAY_"){
			return nil,errors.New("invalid destination identifier or secret name")
		}
		seen[d.ID]=true
		targets=append(targets,delivery.Target{
			ID:d.ID,URL:d.URL,AllowLoopback:d.AllowLoopback,
			Secret:[]byte(os.Getenv(d.SecretEnv)),
		})
	}
	return targets,nil
}
