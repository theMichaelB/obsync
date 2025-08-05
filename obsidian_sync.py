#!/usr/bin/env python3
"""
Obsidian Sync API Client
========================

A Python implementation for interacting with Obsidian Sync API, including:
- TOTP/MFA code generation
- User authentication 
- Vault discovery and access
- Complete vault download with file decryption

Based on reverse engineering of the Obsidian Sync WebSocket protocol.
"""

import base64
import hashlib
import hmac
import json
import requests
import struct
import time
import urllib.parse
import asyncio
import websockets
import ssl
import os
import unicodedata
from typing import Optional, Dict, Any, List
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
from cryptography.hazmat.backends import default_backend
from dotenv import load_dotenv

# Load environment variables
load_dotenv()

class ObsidianSync:
    """Main client for Obsidian Sync API operations."""
    
    def __init__(self, config_file: str = None):
        self.user_email: Optional[str] = None
        self.auth_token: Optional[str] = None
        self.config_file = config_file or os.getenv('CONFIG_FILE', '/app/config/config.json')
        self.config = self.load_config()
        self.session = requests.Session()
        self.api_base = "https://api.obsidian.md"
        self.vault_credentials = self.load_vault_credentials()
        self.sync_base_dir = os.getenv('SYNC_BASE_DIR', '/data/vaults')
    
    def load_config(self) -> dict:
        """Load configuration from JSON file or environment variables."""
        config = {}
        
        # Try to load from file first
        try:
            with open(self.config_file, 'r') as f:
                config = json.load(f)
        except FileNotFoundError:
            print(f"⚠️  Config file {self.config_file} not found, using environment variables")
        except json.JSONDecodeError as e:
            print(f"❌ Error reading config file: {e}")
        
        # Override with environment variables if present
        if os.getenv('OBSIDIAN_EMAIL'):
            config['email'] = os.getenv('OBSIDIAN_EMAIL')
        if os.getenv('OBSIDIAN_PASSWORD'):
            config['password'] = os.getenv('OBSIDIAN_PASSWORD')
        if os.getenv('OBSIDIAN_TOTP_SECRET'):
            config['totp_secret'] = os.getenv('OBSIDIAN_TOTP_SECRET')
        
        self.user_email = config.get('email')
        return config
    
    def load_vault_credentials(self) -> Dict[str, str]:
        """Load vault credentials from file or environment."""
        vault_creds = {}
        
        # Try to load from file
        vault_file = os.getenv('VAULT_CREDENTIALS_FILE', '/app/config/vault_credentials.json')
        try:
            with open(vault_file, 'r') as f:
                vault_creds = json.load(f)
            print(f"📚 Loaded credentials for {len(vault_creds)} vaults from file")
            return vault_creds
        except FileNotFoundError:
            print(f"⚠️  Vault credentials file not found: {vault_file}")
        except json.JSONDecodeError as e:
            print(f"❌ Error reading vault credentials: {e}")
        
        # Try environment variables as fallback
        env_prefix = 'VAULT_PASSWORD_'
        for key, value in os.environ.items():
            if key.startswith(env_prefix):
                vault_name = key[len(env_prefix):].replace('_', ' ')
                vault_creds[vault_name] = value
        
        if vault_creds:
            print(f"📚 Loaded credentials for {len(vault_creds)} vaults from environment")
        
        return vault_creds
    
    def generate_totp_code(self, secret: str, window: int = 30) -> str:
        """
        Generate TOTP (Time-based One-Time Password) code for MFA.
        
        Args:
            secret: Base32-encoded secret key from Obsidian account
            window: Time window in seconds (default 30)
            
        Returns:
            6-digit TOTP code as string
        """
        # Remove any spaces and convert to uppercase
        secret = secret.replace(' ', '').upper()
        
        # Decode base32 secret
        try:
            # Add proper padding for base32
            padding_needed = 8 - (len(secret) % 8)
            if padding_needed != 8:
                secret += '=' * padding_needed
            key = base64.b32decode(secret)
        except Exception as e:
            raise ValueError(f"Invalid TOTP secret format: {e}")
        
        # Calculate time counter
        timestamp = int(time.time() // window)
        
        # Convert timestamp to bytes (big-endian)
        time_bytes = struct.pack('>Q', timestamp)
        
        # Generate HMAC-SHA1 hash
        hmac_hash = hmac.new(key, time_bytes, hashlib.sha1).digest()
        
        # Dynamic truncation
        offset = hmac_hash[-1] & 0x0F
        truncated = struct.unpack('>I', hmac_hash[offset:offset+4])[0]
        truncated &= 0x7FFFFFFF
        
        # Generate 6-digit code
        totp_code = str(truncated % 1000000).zfill(6)
        
        return totp_code
    
    def get_current_mfa_code(self) -> Optional[str]:
        """Get current MFA code using stored TOTP secret."""
        totp_secret = self.config.get('totp_secret')
        if not totp_secret:
            print("❌ No TOTP secret found in config")
            return None
        
        return self.generate_totp_code(totp_secret)
    
    def authenticate(self) -> bool:
        """
        Authenticate with Obsidian API using email, password, and MFA.
        
        Returns:
            True if authentication successful, False otherwise
        """
        email = self.config.get('email')
        password = self.config.get('password')
        
        if not email or not password:
            print("❌ Email or password not found in config")
            return False
        
        # Generate current MFA code
        mfa_code = self.get_current_mfa_code()
        if not mfa_code:
            print("❌ Failed to generate MFA code")
            return False
        
        print(f"🔑 Using MFA code: {mfa_code}")
        
        # Prepare login request
        login_data = {
            "email": email,
            "password": password,
            "mfa": mfa_code
        }
        
        headers = {
            "Content-Type": "application/json",
            "Accept": "*/*",
            "Origin": "app://obsidian.md",
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) obsidian/1.8.10 Chrome/132.0.6834.196 Electron/34.2.0 Safari/537.36"
        }
        
        try:
            print("🌐 Authenticating with Obsidian API...")
            response = self.session.post(
                f"{self.api_base}/user/signin",
                json=login_data,
                headers=headers,
                timeout=30
            )
            
            if response.status_code == 200:
                auth_result = response.json()
                self.auth_token = auth_result.get('token')
                
                if self.auth_token:
                    print("✅ Authentication successful!")
                    print(f"🎫 Token: {self.auth_token[:20]}...")
                    return True
                else:
                    print("❌ No token received in response")
                    return False
            else:
                print(f"❌ Authentication failed: {response.status_code}")
                try:
                    error_data = response.json()
                    print(f"   Error: {error_data}")
                except:
                    print(f"   Response: {response.text}")
                return False
                
        except requests.exceptions.RequestException as e:
            print(f"❌ Network error during authentication: {e}")
            return False
    
    def get_vaults(self) -> Optional[Dict[str, Any]]:
        """
        Get list of available vaults for the authenticated user.
        
        Returns:
            Dictionary containing vault information, or None if failed
        """
        if not self.auth_token:
            print("❌ Not authenticated. Call authenticate() first.")
            return None
        
        headers = {
            "Accept": "*/*",
            "Content-Type": "application/json",
            "Origin": "app://obsidian.md",
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) obsidian/1.8.10 Chrome/132.0.6834.196 Electron/34.2.0 Safari/537.36"
        }
        
        data = {
            "token": self.auth_token,
            "supported_encryption_version": 3
        }
        
        try:
            print("📂 Fetching available vaults...")
            response = self.session.post(
                f"{self.api_base}/vault/list",
                json=data,
                headers=headers,
                timeout=30
            )
            
            if response.status_code == 200:
                vaults_data = response.json()
                vaults = vaults_data.get('vaults', [])
                
                print(f"✅ Found {len(vaults)} vault(s):")
                for i, vault in enumerate(vaults, 1):
                    vault_name = vault.get('name', 'Unknown')
                    vault_id = vault.get('id', 'Unknown')
                    host = vault.get('host', 'Unknown')
                    print(f"   {i}. {vault_name} (ID: {vault_id[:8]}..., Host: {host})")
                
                return vaults_data
            else:
                print(f"❌ Failed to get vaults: {response.status_code}")
                try:
                    error_data = response.json()
                    print(f"   Error: {error_data}")
                except:
                    print(f"   Response: {response.text}")
                return None
                
        except requests.exceptions.RequestException as e:
            print(f"❌ Network error getting vaults: {e}")
            return None
    
    def derive_encryption_key(self, password: str, salt: str) -> tuple:
        """Derive encryption key using scrypt."""
        normalized_password = unicodedata.normalize('NFKC', password)
        normalized_salt = unicodedata.normalize('NFKC', salt)
        
        encryption_key = hashlib.scrypt(
            normalized_password.encode('utf-8'),
            salt=normalized_salt.encode('utf-8'),
            n=32768, r=8, p=1, dklen=32,
            maxmem=128 * 32768 * 8 * 2
        )
        
        keyhash = hashlib.sha256(encryption_key).hexdigest()
        return encryption_key, keyhash
    
    def decrypt_data(self, encrypted_data: bytes, encryption_key: bytes) -> Optional[bytes]:
        """Decrypt data using AES-256-GCM."""
        if len(encrypted_data) < 28:
            return None
        iv = encrypted_data[:12]
        ciphertext = encrypted_data[12:-16]
        tag = encrypted_data[-16:]
        try:
            cipher = Cipher(algorithms.AES(encryption_key), modes.GCM(iv, tag), backend=default_backend())
            decryptor = cipher.decryptor()
            return decryptor.update(ciphertext) + decryptor.finalize()
        except:
            return None
    
    def decrypt_path(self, encrypted_hex: str, encryption_key: bytes) -> Optional[str]:
        """Decrypt file path."""
        try:
            encrypted_data = bytes.fromhex(encrypted_hex)
            decrypted = self.decrypt_data(encrypted_data, encryption_key)
            return decrypted.decode('utf-8') if decrypted else None
        except:
            return None
    
    async def download_single_file(self, websocket, uid: int, filepath: str, expected_size: int, encryption_key: bytes, vault_dir: str) -> bool:
        """Download a single file using UID method with support for large files."""
        print(f"📥 Downloading: {filepath} (uid:{uid}, {expected_size} bytes)")
        
        # For large files, we may need to handle chunked responses
        if expected_size > 1000000:  # 1MB
            print(f"   📦 Large file detected, using chunked download")
        
        # Send pull request
        pull_msg = {"op": "pull", "uid": uid}
        await websocket.send(json.dumps(pull_msg))
        
        # Wait for metadata and binary content
        waiting_for_metadata = True
        binary_chunks = []
        timeout_count = 0
        max_timeout = 100  # 10 seconds for large files
        metadata = None
        
        while timeout_count < max_timeout:
            try:
                response = await asyncio.wait_for(websocket.recv(), timeout=0.5)
                
                try:
                    # Try to parse as JSON (metadata)
                    data = json.loads(response)
                    
                    if waiting_for_metadata and 'hash' in data and 'size' in data:
                        metadata = data
                        file_size = data.get('size', 0)
                        pieces = data.get('pieces', 0)
                        
                        if pieces == 0:
                            # Empty file
                            self.save_file(filepath, "", vault_dir)
                            return True
                        
                        waiting_for_metadata = False
                        print(f"   📋 Metadata received: {pieces} piece(s), {file_size} bytes")
                        
                    elif data.get('op') == 'ping':
                        await websocket.send(json.dumps({"op": "pong"}))
                        
                except (json.JSONDecodeError, UnicodeDecodeError):
                    # Binary data
                    if not waiting_for_metadata:
                        # Convert to bytes
                        if isinstance(response, str):
                            binary_data = response.encode('latin-1')
                        else:
                            binary_data = response
                        
                        binary_chunks.append(binary_data)
                        
                        # Check if we have all pieces
                        if metadata and len(binary_chunks) >= metadata.get('pieces', 1):
                            # Combine all chunks
                            full_binary = b''.join(binary_chunks)
                            
                            # Decrypt content
                            decrypted = self.decrypt_data(full_binary, encryption_key)
                            
                            if decrypted:
                                try:
                                    # Try as text
                                    text_content = decrypted.decode('utf-8')
                                    self.save_file(filepath, text_content, vault_dir)
                                    return True
                                    
                                except UnicodeDecodeError:
                                    # Binary file
                                    self.save_file(filepath, decrypted, vault_dir, is_binary=True)
                                    return True
                            else:
                                print(f"   ❌ Decryption failed")
                                return False
                
            except asyncio.TimeoutError:
                timeout_count += 1
                # For large files, keep waiting longer
                if binary_chunks and expected_size > 1000000:
                    max_timeout = 200  # 20 seconds for very large files
                continue
                
        print(f"   ❌ Timeout waiting for {filepath} (received {len(binary_chunks)} chunks)")
        return False
    
    def save_file(self, filepath: str, content, vault_dir: str, is_binary: bool = False):
        """Save file to disk."""
        safe_filepath = filepath.replace('..', '').replace('\\', '/').lstrip('/')
        full_path = os.path.join(vault_dir, safe_filepath)
        
        os.makedirs(os.path.dirname(full_path), exist_ok=True)
        
        try:
            mode = 'wb' if is_binary else 'w'
            encoding = None if is_binary else 'utf-8'
            
            with open(full_path, mode, encoding=encoding, errors='replace' if not is_binary else None) as f:
                f.write(content)
            
            return True
        except Exception as e:
            print(f"❌ Save error for {safe_filepath}: {e}")
            return False
    
    async def sync_single_vault(self, vault_data: Dict[str, Any], password: str) -> bool:
        """Sync a single vault."""
        vault_name = vault_data.get('name', 'Unknown')
        vault_id = vault_data.get('id')
        host = vault_data.get('host')
        salt = vault_data.get('salt')
        
        print(f"\n🎯 Starting sync for vault: {vault_name}")
        
        # Create vault directory
        vault_dir = os.path.join(self.sync_base_dir, vault_name.replace('/', '_'))
        os.makedirs(vault_dir, exist_ok=True)
        
        # Derive encryption key
        encryption_key, keyhash = self.derive_encryption_key(password, salt)
        
        print(f"🔗 Connecting to: wss://{host}/")
        
        headers = {
            "Origin": "app://obsidian.md",
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) obsidian/1.8.10 Chrome/132.0.6834.196 Electron/34.2.0 Safari/537.36"
        }
        
        ssl_context = ssl.create_default_context()
        
        try:
            async with websockets.connect(
                f"wss://{host}/",
                ssl=ssl_context,
                extra_headers=headers,  # Changed from additional_headers
                compression="deflate",
                max_size=10 * 1024 * 1024  # 10MB max frame size
            ) as websocket:
                print("✅ Connected!")
                
                # Initialize
                init_msg = {
                    "op": "init",
                    "token": self.auth_token,
                    "id": vault_id,
                    "keyhash": keyhash,
                    "version": 0,
                    "initial": True,
                    "device": "ObsidianSyncPython",
                    "encryption_version": 0
                }
                
                await websocket.send(json.dumps(init_msg))
                print("📤 Sent init")
                
                # Phase 1: Collect all files
                files_to_download = []
                
                async for message in websocket:
                    try:
                        data = json.loads(message)
                        
                        if data.get('res') == 'ok':
                            print("✅ Authentication successful!")
                            
                        elif data.get('op') == 'push':
                            encrypted_path = data.get('path', '')
                            is_folder = data.get('folder', False)
                            size = data.get('size', 0)
                            uid = data.get('uid', 0)
                            deleted = data.get('deleted', False)
                            
                            if deleted:
                                continue
                                
                            decrypted_path = self.decrypt_path(encrypted_path, encryption_key)
                            
                            if is_folder:
                                if decrypted_path:
                                    os.makedirs(os.path.join(vault_dir, decrypted_path), exist_ok=True)
                                continue
                            
                            if size == 0:
                                # Handle empty files immediately
                                if decrypted_path:
                                    self.save_file(decrypted_path, "", vault_dir)
                                continue
                            
                            # Add to download queue
                            if decrypted_path:
                                files_to_download.append({
                                    'uid': uid,
                                    'path': decrypted_path,
                                    'size': size
                                })
                        
                        elif data.get('op') == 'ready':
                            print(f"🔄 Server ready! Found {len(files_to_download)} files to download")
                            break
                            
                        elif data.get('op') == 'ping':
                            await websocket.send(json.dumps({"op": "pong"}))
                            
                    except json.JSONDecodeError:
                        # Ignore binary data during discovery
                        pass
                
                # Phase 2: Download all files
                print(f"\n📥 Starting downloads...")
                success_count = 0
                
                for i, file_info in enumerate(files_to_download):
                    print(f"\n[{i+1}/{len(files_to_download)}]", end=" ")
                    
                    success = await self.download_single_file(
                        websocket, 
                        file_info['uid'], 
                        file_info['path'], 
                        file_info['size'],
                        encryption_key,
                        vault_dir
                    )
                    
                    if success:
                        success_count += 1
                    
                    # Small delay between downloads
                    await asyncio.sleep(0.1)
                
                print(f"\n🎉 {vault_name} sync complete: {success_count}/{len(files_to_download)} files")
                return success_count > 0
        
        except Exception as e:
            print(f"❌ Connection error for {vault_name}: {e}")
            return False
    
    async def sync_specific_vault(self, vault_name: str) -> bool:
        """Sync a specific vault by name."""
        if not self.auth_token:
            print("❌ Not authenticated. Authenticating...")
            if not self.authenticate():
                return False
        
        # Get vault list
        vaults_data = self.get_vaults()
        if not vaults_data:
            print("❌ Failed to get vault list")
            return False
        
        vaults = vaults_data.get('vaults', [])
        
        # Find the specific vault
        target_vault = None
        for vault in vaults:
            if vault.get('name') == vault_name:
                target_vault = vault
                break
        
        if not target_vault:
            print(f"❌ Vault '{vault_name}' not found")
            return False
        
        # Get password for this vault
        password = self.vault_credentials.get(vault_name)
        if not password:
            print(f"❌ No password found for vault: {vault_name}")
            return False
        
        # Create base sync directory
        os.makedirs(self.sync_base_dir, exist_ok=True)
        
        # Sync the vault
        success = await self.sync_single_vault(target_vault, password)
        
        if success:
            print(f"✅ Vault '{vault_name}' synced successfully")
        else:
            print(f"❌ Failed to sync vault '{vault_name}'")
        
        return success
    
    async def sync_all_vaults(self) -> bool:
        """Sync all vaults to local directory."""
        if not self.auth_token:
            print("❌ Not authenticated. Authenticating...")
            if not self.authenticate():
                return False
        
        # Get vault list
        vaults_data = self.get_vaults()
        if not vaults_data:
            print("❌ Failed to get vault list")
            return False
        
        vaults = vaults_data.get('vaults', [])
        
        # Create base sync directory
        os.makedirs(self.sync_base_dir, exist_ok=True)
        print(f"\n📁 Syncing to: {self.sync_base_dir}")
        
        # Sync each vault
        success_count = 0
        for vault in vaults:
            vault_name = vault.get('name', 'Unknown')
            
            # Get password for this vault
            password = self.vault_credentials.get(vault_name)
            if not password:
                print(f"\n⚠️  No password found for vault: {vault_name}, skipping")
                continue
            
            success = await self.sync_single_vault(vault, password)
            if success:
                success_count += 1
        
        print(f"\n🎆 Sync complete! {success_count}/{len(vaults)} vaults synced successfully")
        return success_count > 0
    
    def test_totp_generation(self, secret: str) -> None:
        """
        Test TOTP generation and display current code with timing info.
        
        Args:
            secret: Base32-encoded TOTP secret
        """
        try:
            # Generate current code
            code = self.generate_totp_code(secret)
            
            # Calculate time remaining in current window
            current_time = int(time.time())
            window_start = (current_time // 30) * 30
            time_remaining = 30 - (current_time - window_start)
            
            print(f"🔑 Current TOTP Code: {code}")
            print(f"⏱️  Time remaining: {time_remaining} seconds")
            print(f"🕐 Next code in: {time_remaining} seconds")
            
            # Show next code for verification
            next_timestamp = int(time.time() // 30) + 1
            next_time_bytes = struct.pack('>Q', next_timestamp)
            clean_secret = secret.replace(' ', '').upper()
            padding_needed = 8 - (len(clean_secret) % 8)
            if padding_needed != 8:
                clean_secret += '=' * padding_needed
            key = base64.b32decode(clean_secret)
            next_hmac = hmac.new(key, next_time_bytes, hashlib.sha1).digest()
            next_offset = next_hmac[-1] & 0x0F
            next_truncated = struct.unpack('>I', next_hmac[next_offset:next_offset+4])[0]
            next_truncated &= 0x7FFFFFFF
            next_code = str(next_truncated % 1000000).zfill(6)
            
            print(f"🔮 Next code will be: {next_code}")
            
        except Exception as e:
            print(f"❌ Error generating TOTP: {e}")

def main():
    """Interactive CLI for Obsidian Sync operations."""
    print("🔐 Obsidian Sync API Client")
    print("=" * 40)
    
    client = ObsidianSync()
    
    # Check if we have config
    if not client.config.get('totp_secret'):
        print("❌ No configuration found. Please create config.json with credentials.")
        return
    
    print(f"📧 Using credentials for: {client.user_email}")
    
    while True:
        print("\n📋 Available operations:")
        print("1. 🔑 Generate TOTP/MFA code")
        print("2. 🌐 Authenticate with Obsidian API")
        print("3. 📂 List available vaults")
        print("4. 📥 Sync all vaults to local directory")
        print("5. ❌ Exit")
        
        choice = input("\nSelect operation (1-5): ").strip()
        
        if choice == '1':
            print("\n📱 Generating TOTP code...")
            client.test_totp_generation(client.config['totp_secret'])
            
        elif choice == '2':
            print("\n🌐 Authenticating...")
            if client.authenticate():
                print("🎉 Ready to access vaults!")
            else:
                print("💡 Tip: Authentication uses current TOTP code")
                
        elif choice == '3':
            print("\n📂 Getting vaults...")
            vaults = client.get_vaults()
            if vaults:
                print("💡 Tip: Use option 4 to download a specific vault")
                
        elif choice == '4':
            print("\n📥 Starting vault sync...")
            asyncio.run(client.sync_all_vaults())
            
        elif choice == '5':
            print("\n👋 Goodbye!")
            break
            
        else:
            print("❌ Invalid choice. Please select 1-5.")
            
        input("\nPress Enter to continue...")
        print("\n" + "="*50)

if __name__ == "__main__":
    main()