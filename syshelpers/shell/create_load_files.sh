#!/bin/bash
# for i in 4 8 16 32 64 128 512 1024 2048 4096; do sh ../../syshelpers/shell/create_load_files.sh 100 ./client0/${i}k/ ${i}; done

MAX=$1
DIR=$2
SIZ=$3

mkdir -p "${DIR}"

if command -v python3 >/dev/null 2>&1; then
    python3 -c "
import sys, os, uuid
max_files = int(sys.argv[1])
out_dir = sys.argv[2]
size_kb = int(sys.argv[3])
buf = os.urandom(size_kb * 1024)
for _ in range(max_files):
    name = str(uuid.uuid4())
    with open(os.path.join(out_dir, name), 'wb') as f:
        f.write(buf)
" "$MAX" "$DIR" "$SIZ"
    exit 0
fi

for i in $(seq $MAX)
do
    name=$(uuidgen)
    dd if=/dev/urandom of="${DIR}/$name" bs=${SIZ}k count=1
done

