#!/usr/bin/env python3
import base64
import hmac
import hashlib
import struct
import time

def generate_totp(secret):
    # Clean secret
    secret = secret.replace(' ', '').upper()
    
    # Add padding if needed
    padding_needed = 8 - (len(secret) % 8)
    if padding_needed != 8:
        secret += '=' * padding_needed
    
    # Decode
    key = base64.b32decode(secret)
    
    # Get timestamp
    timestamp = int(time.time() // 30)
    
    # Convert to bytes
    time_bytes = struct.pack('>Q', timestamp)
    
    # HMAC
    hmac_hash = hmac.new(key, time_bytes, hashlib.sha1).digest()
    
    # Dynamic truncation
    offset = hmac_hash[-1] & 0x0F
    truncated = struct.unpack('>I', hmac_hash[offset:offset+4])[0]
    truncated &= 0x7FFFFFFF
    
    # Generate 6-digit code
    totp_code = str(truncated % 1000000).zfill(6)
    
    return totp_code

if __name__ == "__main__":
    secret = "EE6SJEB23D5T6J47EU6Z3F2WKY2HHMMX"
    code = generate_totp(secret)
    print(code)