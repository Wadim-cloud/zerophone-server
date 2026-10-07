#!/bin/bash

cd ~/Documents/Dev/zerophone

echo "📦 Building ZeroPhone..."
go build -o zerophone .

echo "📦 Building ZeroPhone SIP Bridge..."
cd cmd/zerobridge
go build -o ../../zerobridge .
cd ../..

if [ -f ./zerobridge ]; then
    echo "✅ Build complete!"
    ls -lh zerophone zerobridge
else
    echo "❌ Build failed"
    exit 1
fi
