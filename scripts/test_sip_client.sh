#!/bin/bash

# test_sip_client.sh - Test SIP calls without external provider

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}=== ZeroPhone SIP Test Suite ===${NC}"

# Check if SIPp is installed
if ! command -v sipp &> /dev/null; then
    echo -e "${RED}SIPp not found. Installing...${NC}"
    
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        brew install sipp
    elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
        # Linux
        sudo apt-get update
        sudo apt-get install -y sipp
    else
        echo -e "${RED}Please install SIPp manually${NC}"
        exit 1
    fi
fi

# Get local IP
LOCAL_IP=$(ifconfig | grep "inet " | grep -v 127.0.0.1 | head -1 | awk '{print $2}')
if [ -z "$LOCAL_IP" ]; then
    LOCAL_IP="127.0.0.1"
fi

echo -e "${GREEN}Local IP: ${LOCAL_IP}${NC}"

# Create SIPp scenario files
mkdir -p /tmp/sipp-scenarios

cat > /tmp/sipp-scenarios/incoming.xml << 'EOF'
<?xml version="1.0" encoding="ISO-8859-1" ?>
<!DOCTYPE scenario SYSTEM "sipp.dtd">
<scenario name="incoming-call">
  <send retrans="500">
    <![CDATA[
      INVITE sip:[service]@[remote_ip]:5060 SIP/2.0
      Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
      From: "SIPp Test" <sip:sipp@[local_ip]>;tag=[call_number]
      To: <sip:[service]@[remote_ip]>
      Call-ID: [call_id]
      CSeq: 1 INVITE
      Contact: <sip:sipp@[local_ip]:[local_port]>
      Content-Type: application/sdp
      Max-Forwards: 70
      Subject: Test Call
      Content-Length: [len]
      
      v=0
      o=user1 53655765 2353687637 IN IP[local_ip_type] [local_ip]
      s=-
      t=0 0
      c=IN IP[local_ip_type] [local_ip]
      m=audio [media_port] RTP/AVP 0 8 101
      a=rtpmap:0 PCMU/8000
      a=rtpmap:8 PCMA/8000
      a=rtpmap:101 telephone-event/8000
      a=sendrecv
    ]]>
  </send>

  <recv response="100" optional="true" rtd="true"/>
  <recv response="180" optional="true"/>
  <recv response="183" optional="true"/>
  
  <recv response="200" rtd="true">
  </recv>

  <send>
    <![CDATA[
      ACK sip:[service]@[remote_ip]:5060 SIP/2.0
      Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
      From: "SIPp Test" <sip:sipp@[local_ip]>;tag=[call_number]
      To: [service] <sip:[service]@[remote_ip]>[peer_tag_param]
      Call-ID: [call_id]
      CSeq: 1 ACK
      Contact: sip:sipp@[local_ip]:[local_port]
      Content-Length: 0
    ]]>
  </send>

  <pause milliseconds="5000"/>

  <send>
    <![CDATA[
      BYE sip:[service]@[remote_ip]:5060 SIP/2.0
      Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
      From: "SIPp Test" <sip:sipp@[local_ip]>;tag=[call_number]
      To: [service] <sip:[service]@[remote_ip]>[peer_tag_param]
      Call-ID: [call_id]
      CSeq: 2 BYE
      Contact: sip:sipp@[local_ip]:[local_port]
      Content-Length: 0
    ]]>
  </send>

  <recv response="200" optional="true">
  </recv>
</scenario>
EOF

cat > /tmp/sipp-scenarios/register.xml << 'EOF'
<?xml version="1.0" encoding="ISO-8859-1" ?>
<!DOCTYPE scenario SYSTEM "sipp.dtd">
<scenario name="register">
  <send retrans="500">
    <![CDATA[
      REGISTER sip:[remote_ip] SIP/2.0
      Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
      From: <sip:[service]@[remote_ip]>;tag=[call_number]
      To: <sip:[service]@[remote_ip]>
      Call-ID: [call_id]
      CSeq: 1 REGISTER
      Contact: <sip:[service]@[local_ip]:[local_port]>
      Expires: 3600
      Content-Length: 0
    ]]>
  </send>

  <recv response="200" optional="true">
  </recv>
</scenario>
EOF

echo -e "${GREEN}Created SIPp scenarios${NC}"

# Function to test single call
test_single_call() {
    local EXTENSION=$1
    echo -e "${YELLOW}Testing call to extension ${EXTENSION}...${NC}"
    
    sipp -sf /tmp/sipp-scenarios/incoming.xml \
         -s $EXTENSION \
         -i $LOCAL_IP \
         -p 5061 \
         -m 1 \
         -l 1 \
         -trace_msg \
         -message_file /tmp/sipp_messages.log \
         $LOCAL_IP:5060
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Call to ${EXTENSION} successful${NC}"
    else
        echo -e "${RED}✗ Call to ${EXTENSION} failed${NC}"
    fi
}

# Function to test multiple calls
test_multiple_calls() {
    local EXTENSION=$1
    local COUNT=$2
    echo -e "${YELLOW}Testing ${COUNT} concurrent calls to ${EXTENSION}...${NC}"
    
    sipp -sf /tmp/sipp-scenarios/incoming.xml \
         -s $EXTENSION \
         -i $LOCAL_IP \
         -p 5061 \
         -m $COUNT \
         -l $COUNT \
         -r 10 \
         -rp 1000 \
         $LOCAL_IP:5060
}

# Function to register an extension via API
register_extension() {
    local EXTENSION=$1
    local USER_ID=$2
    local POLICY=${3:-"single"}
    
    echo -e "${YELLOW}Registering extension ${EXTENSION} -> ${USER_ID} (${POLICY})${NC}"
    
    curl -s -X POST http://localhost:9443/sip/register \
         -H "Content-Type: application/json" \
         -d "{\"extension\":\"${EXTENSION}\",\"user_id\":\"${USER_ID}\",\"policy\":\"${POLICY}\"}"
    
    echo -e "\n${GREEN}✓ Extension registered${NC}"
}

# Main menu
show_menu() {
    echo ""
    echo -e "${GREEN}=== SIP Test Menu ===${NC}"
    echo "1) Register test extension"
    echo "2) Make single test call"
    echo "3) Make multiple test calls"
    echo "4) Show registered extensions"
    echo "5) Unregister extension"
    echo "6) Monitor call status"
    echo "7) Run full integration test"
    echo "8) Exit"
    echo ""
}

# Check if ZeroPhone is running
check_zerophone() {
    if ! curl -s http://localhost:9443/status > /dev/null 2>&1; then
        echo -e "${RED}ZeroPhone is not running! Start it with: go run .${NC}"
        exit 1
    fi
}

# Check if zerobridge is running
check_zerobridge() {
    if ! nc -z localhost 5060 2>/dev/null; then
        echo -e "${RED}zerobridge is not running! Start it with: go run ./cmd/zerobridge${NC}"
        exit 1
    fi
}

# Main loop
check_zerophone
check_zerobridge

while true; do
    show_menu
    read -p "Choice: " choice
    
    case $choice in
        1)
            read -p "Extension (e.g., 1001): " ext
            read -p "User ID: " userid
            read -p "Policy [single/shared]: " policy
            register_extension $ext $userid $policy
            ;;
        2)
            read -p "Extension to call: " ext
            test_single_call $ext
            ;;
        3)
            read -p "Extension to call: " ext
            read -p "Number of calls: " count
            test_multiple_calls $ext $count
            ;;
        4)
            echo -e "${GREEN}Registered extensions:${NC}"
            curl -s http://localhost:9443/sip/map | jq '.'
            ;;
        5)
            read -p "Extension to unregister: " ext
            curl -s -X POST http://localhost:9443/sip/unregister \
                 -H "Content-Type: application/json" \
                 -d "{\"extension\":\"${ext}\"}"
            echo -e "${GREEN}✓ Extension unregistered${NC}"
            ;;
        6)
            echo -e "${GREEN}Call status:${NC}"
            curl -s http://localhost:9443/sip/sessions | jq '.'
            echo ""
            echo -e "${GREEN}Media workers:${NC}"
            curl -s http://localhost:9443/sip/media/workers | jq '.'
            ;;
        7)
            echo -e "${GREEN}Running full integration test...${NC}"
            
            # Register test extension
            register_extension "9999" "sipp-test-user" "single"
            
            # Wait a moment
            sleep 2
            
            # Make test call
            test_single_call "9999"
            
            # Show results
            echo -e "\n${GREEN}Call sessions:${NC}"
            curl -s http://localhost:9443/sip/sessions | jq '.'
            ;;
        8)
            echo -e "${GREEN}Goodbye!${NC}"
            exit 0
            ;;
        *)
            echo -e "${RED}Invalid choice${NC}"
            ;;
    esac
done