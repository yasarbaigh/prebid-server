import sys
import os

# Add the current directory to sys.path so we can import the generated proto
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

try:
    from proto import auction_log_pb2
    from google.protobuf.json_format import MessageToJson
except ImportError as e:
    print(f"Error: Missing dependencies. Please install: pip install protobuf")
    print(f"Details: {e}")
    sys.exit(1)

def decode_log_line(hex_line):
    try:
        # 1. Clean the line (remove whitespace/newlines)
        hex_line = hex_line.strip()
        if not hex_line:
            return None
        
        # 2. Convert Hex to Bytes
        binary_data = bytes.fromhex(hex_line)
        
        # 3. Parse Protobuf
        event = auction_log_pb2.AuctionEvent()
        event.ParseFromString(binary_data)
        
        return event
    except Exception as e:
        return f"Error decoding line: {e}"

def main():
    if len(sys.argv) < 2:
        print("Usage: python3 pb_decoder.py <path_to_log_file> [num_lines]")
        print("Example: python3 pb_decoder.py /opt/adserving/logs/ssp_bids_1/ssp_bids_response.log 5")
        sys.exit(1)

    log_path = sys.argv[1]
    num_lines = int(sys.argv[2]) if len(sys.argv) > 2 else 0

    if not os.path.exists(log_path):
        print(f"Error: File not found: {log_path}")
        sys.exit(1)

    count = 0
    with open(log_path, 'r') as f:
        for line in f:
            event = decode_log_line(line)
            if event:
                if isinstance(event, str):
                    print(event)
                else:
                    # Print as clean JSON for readability
                    print(f"--- Event {count + 1} ---")
                    print(MessageToJson(event))
                
                count += 1
                if num_lines > 0 and count >= num_lines:
                    break

    print(f"\nProcessed {count} events.")

if __name__ == "__main__":
    main()
