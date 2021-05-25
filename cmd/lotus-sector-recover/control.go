package main

import (
	"fmt"
	"net/http"

	"github.com/urfave/cli/v2"
)

var ControlCmd = &cli.Command{
	Name:  "control",
	Usage: "control p1,p2,push",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "p1_file",
			Usage: "speific sector_number_file,seperate with \\r\\n",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "dIndex",
			Usage: "speific deadline index",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "p2_file",
			Usage: "p1 Output,json format",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "push_file",
			Usage: "speific file ,json format",
			Value: "",
		},
	},
	Action: func(cctx *cli.Context) error {
		p1_path := cctx.String("p1_file")
		dIndex := cctx.String("dIndex")
		if len(p1_path) > 0 || len(dIndex) > 0 {
			resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:9999/p1?path=%s&&dIndex=%s", p1_path, dIndex))
			if err != nil {
				log.Error("p1 get request error : ", err.Error())
				return err
			}
			resp.Body.Close()
		}
		p2_path := cctx.String("p2_file")
		if len(p2_path) > 0 {
			resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:9999/p2?path=%s", p2_path))
			if err != nil {
				log.Error("p1 get request error : ", err.Error())
				return err
			}
			resp.Body.Close()
		}
		push_file := cctx.String("push_file")
		if len(push_file) > 0 {
			resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:9999/push?path=%s", push_file))
			if err != nil {
				log.Error("p1 get request error : ", err.Error())
				return err
			}
			resp.Body.Close()
		}
		return nil
	},
}
