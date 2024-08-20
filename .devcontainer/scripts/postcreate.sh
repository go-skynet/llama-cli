#!/bin/bash

cd /workspace

if [ ! -d ".git" ]; then
    git clone https://github.com/mudler/LocalAI.git .
else
    git fetch
fi

echo "Standard Post-Create script completed."

if [ -f "/devcontainer-customization/postcreate.sh"]; then
    echo "Launching customization postcreate.sh"
    bash "/devcontainer-customization/postcreate.sh"
fi