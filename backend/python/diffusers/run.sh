#!/bin/bash

##
## A bash script wrapper that runs the diffusers server with conda

export PATH=$PATH:/opt/conda/bin

if [ -d "/opt/intel" ]; then
    # Assumes we are using the Intel oneAPI container image
    # https://github.com/intel/intel-extension-for-pytorch/issues/538
    export XPU=1
else
    # Activate conda environment
    source activate diffusers
fi

# get the directory where the bash script is located
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

# Intel image: If there is no "python" command, try "python3"
if ! [ -x "$(command -v python)" ]; then
  if [ -x "$(command -v python3)" ]; then
    export PYTHON=python3
  else
    echo 'Error: python is not installed.' >&2
    exit 1
  fi
else
  export PYTHON=python
fi

$PYTHON $DIR/backend_diffusers.py $@
