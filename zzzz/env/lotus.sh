sdt0111=/data/lotus/dev/.sdt0111 # $(mktemp -d)
staging=/data/lotus/dev/.staging # $(mktemp -d)
ldt0111=/data/lotus/dev/.ldt0111
mdt0111=/data/lotus/dev/.mdt0111 # $(mktemp -d)

export LOTUS_PATH="${ldt0111}" 
export LOTUS_MINER_PATH="${mdt0111}"
export PATH=$PATH:$HOME/git/sn-lotus



netip=$(ip a | grep -Po '(?<=inet ).*(?=\/)'|grep -E "10\.|192\.") # only support one eth card.
if [ -z "$netip" ]; then
    netip="127.0.0.1"
fi

export NETIP=$netip 