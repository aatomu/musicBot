screen -r music -X kill
screen -U -A -md -S music
screen -r music -X stuff "cd $(dirname $0)/\n"
screen -r music -X stuff "while :; do go run main.go -prefix=\!music -token=ODcyMzU5MTI3MTgyMDk0Mzc3.YQotvw.i4_00VV9GSCsd_ypn1I3R-CfwJU ; done\n"