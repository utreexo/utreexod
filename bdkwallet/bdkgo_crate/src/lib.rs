use std::{
    io::Read,
    str::FromStr,
    sync::{Arc, Mutex},
};

pub use bdk::wallet::Balance;
use bdk::{
    bitcoin::{
        self, consensus::Decodable, hashes::Hash, network::constants::ParseNetworkError, BlockHash,
        Network,
    },
    keys::{DerivableKey, ExtendedKey},
    template::Bip86,
    wallet::AddressIndex,
    KeychainKind,
};
use bincode::Options;
use rand::RngCore;

uniffi::include_scaffolding!("bdkgo");

const DB_MAGIC: &str = "utreexod.bdk.345e94cf";
const DB_MAGIC_LEN: usize = DB_MAGIC.len();
const ENTROPY_LEN: usize = 16; // 12 words

type BdkWallet = bdk::Wallet<bdk_file_store::Store<bdk::wallet::ChangeSet>>;

fn bincode_config() -> impl bincode::Options {
    bincode::options().with_fixint_encoding()
}

#[derive(Debug, thiserror::Error)]
pub enum CreateNewError {
    #[error("failed to parse network type string: {0}")]
    ParseNetwork(ParseNetworkError),
    #[error("failed to parse genesis hash: {0}")]
    ParseGenesisHash(bdk::bitcoin::hashes::Error),
    #[error("failed to create new db file: {0}")]
    Database(bdk_file_store::FileError<'static>),
    #[error("failed to init wallet: {0}")]
    Wallet(bdk::wallet::NewError<std::io::Error>),
}

#[derive(Debug, thiserror::Error)]
pub enum LoadError {
    #[error("failed to load db: {0}")]
    Database(bdk_file_store::FileError<'static>),
    #[error("failed to decode wallet header: {0}")]
    ParseHeader(bincode::Error),
    #[error("wallet header version unsupported")]
    HeaderVersion,
    #[error("failed to init wallet: {0}")]
    Wallet(bdk::wallet::LoadError<bdk_file_store::IterError>),
}

#[derive(Debug, thiserror::Error)]
pub enum DatabaseError {
    #[error("failed to write to db: {0}")]
    Write(std::io::Error),
}

#[derive(Debug, thiserror::Error)]
pub enum ApplyBlockError {
    #[error("failed to decode block: {0}")]
    DecodeBlock(bdk::bitcoin::consensus::encode::Error),
    #[error("block cannot connect with wallet's chain: {0}")]
    CannotConnect(bdk::chain::local_chain::CannotConnectError),
    #[error("failed to write block to db: {0}")]
    Database(std::io::Error),
}

pub struct AddressInfo {
    pub index: u32,
    pub address: String,
}

#[derive(Debug, serde::Serialize, serde::Deserialize)]
pub struct WalletHeader {
    pub version: [u8; DB_MAGIC_LEN],
    pub entropy: [u8; ENTROPY_LEN],
    pub network: Network,
}

impl WalletHeader {
    pub fn new(network: Network) -> Self {
        let mut version = [0_u8; DB_MAGIC_LEN];
        version.copy_from_slice(DB_MAGIC.as_bytes());
        let mut entropy = [0_u8; ENTROPY_LEN];
        rand::thread_rng().fill_bytes(&mut entropy);
        Self {
            version,
            entropy,
            network,
        }
    }

    pub fn encode(&mut self) -> Vec<u8> {
        self.version.copy_from_slice(DB_MAGIC.as_bytes());
        let b = bincode_config()
            .serialize(&self)
            .expect("bincode must serialize");
        let l = (b.len() as u32).to_le_bytes();
        l.into_iter().chain(b).collect::<Vec<u8>>()
    }

    pub fn decode<R: Read>(mut r: R) -> Result<(Self, Vec<u8>), LoadError> {
        let mut l_buf = [0_u8; 4];
        r.read_exact(&mut l_buf)
            .map_err(|err| LoadError::Database(bdk_file_store::FileError::Io(err)))?;
        let l = u32::from_le_bytes(l_buf);
        let mut b = vec![0; l as usize];
        r.read_exact(&mut b)
            .map_err(|err| LoadError::Database(bdk_file_store::FileError::Io(err)))?;

        let header = bincode_config()
            .deserialize::<WalletHeader>(&b)
            .map_err(LoadError::ParseHeader)?;
        if header.version != DB_MAGIC.as_bytes() {
            return Err(LoadError::HeaderVersion);
        }

        let header_b = l_buf.into_iter().chain(b).collect::<Vec<u8>>();
        Ok((header, header_b))
    }

    pub fn descriptor(&self, keychain: KeychainKind) -> Bip86<ExtendedKey<bdk::miniscript::Tap>> {
        let mnemonic =
            bdk::keys::bip39::Mnemonic::from_entropy(&self.entropy).expect("must get mnemonic");
        let mut ext_key: bdk::keys::ExtendedKey<bdk::miniscript::Tap> = mnemonic
            .into_extended_key()
            .expect("must become extended key");
        match &mut ext_key {
            ExtendedKey::Private((key, _)) => key.network = self.network,
            ExtendedKey::Public((key, _)) => key.network = self.network,
        };
        Bip86(ext_key, keychain)
    }
}

pub struct Wallet {
    inner: Mutex<BdkWallet>,
}

impl Wallet {
    pub fn create_new(
        db_path: String,
        network: String,
        genesis_hash: Vec<u8>,
    ) -> Result<Self, CreateNewError> {
        let network = Network::from_str(&network).map_err(CreateNewError::ParseNetwork)?;
        let genesis_hash =
            BlockHash::from_slice(&genesis_hash).map_err(CreateNewError::ParseGenesisHash)?;

        let mut db_header = WalletHeader::new(network);
        let db_header_bytes = Box::new(db_header.encode()).leak();

        let db = match bdk_file_store::Store::create_new(db_header_bytes, &db_path) {
            Ok(db) => db,
            Err(err) => return Err(CreateNewError::Database(err)),
        };

        let bdk_wallet = match bdk::Wallet::new_with_genesis_hash(
            db_header.descriptor(KeychainKind::External),
            Some(db_header.descriptor(KeychainKind::Internal)),
            db,
            network,
            genesis_hash,
        ) {
            Ok(w) => w,
            Err(err) => {
                let _ = std::fs::remove_file(db_path);
                return Err(CreateNewError::Wallet(err));
            }
        };

        Ok(Self {
            inner: Mutex::new(bdk_wallet),
        })
    }

    pub fn load(db_path: String) -> Result<Self, LoadError> {
        let file = std::fs::File::open(&db_path)
            .map_err(|err| LoadError::Database(bdk_file_store::FileError::Io(err)))?;
        let (db_header, db_header_bytes) = WalletHeader::decode(file)?;
        let db_header_bytes = Box::new(db_header_bytes).leak();
        let db =
            bdk_file_store::Store::open(db_header_bytes, db_path).map_err(LoadError::Database)?;
        let bdk_wallet = bdk::Wallet::load(
            db_header.descriptor(KeychainKind::External),
            Some(db_header.descriptor(KeychainKind::Internal)),
            db,
        )
        .map_err(LoadError::Wallet)?;

        Ok(Self {
            inner: Mutex::new(bdk_wallet),
        })
    }

    fn address(self: Arc<Self>, index: AddressIndex) -> Result<AddressInfo, DatabaseError> {
        let mut wallet = self.inner.lock().unwrap();
        let address_info = wallet
            .try_get_address(index)
            .map_err(DatabaseError::Write)?;
        Ok(AddressInfo {
            index: address_info.index,
            address: address_info.address.to_string(),
        })
    }

    pub fn last_unused_address(self: Arc<Self>) -> Result<AddressInfo, DatabaseError> {
        self.address(AddressIndex::LastUnused)
    }

    pub fn fresh_address(self: Arc<Self>) -> Result<AddressInfo, DatabaseError> {
        self.address(AddressIndex::New)
    }

    pub fn peek_address(self: Arc<Self>, index: u32) -> Result<AddressInfo, DatabaseError> {
        self.address(AddressIndex::Peek(index))
    }

    pub fn balance(self: Arc<Self>) -> bdk::wallet::Balance {
        let wallet = self.inner.lock().unwrap();
        wallet.get_balance()
    }

    pub fn genesis_hash(self: Arc<Self>) -> Vec<u8> {
        self.inner
            .lock()
            .unwrap()
            .local_chain()
            .genesis_hash()
            .to_byte_array()
            .to_vec()
    }

    pub fn recent_blocks(self: Arc<Self>, count: u32) -> Vec<BlockId> {
        let tip = self.inner.lock().unwrap().latest_checkpoint();
        tip.into_iter()
            .take(count as _)
            .map(|cp| BlockId {
                height: cp.height(),
                hash: cp.hash().to_byte_array().to_vec(),
            })
            .collect()
    }

    pub fn apply_block(
        self: Arc<Self>,
        height: u32,
        block: Arc<Block>,
    ) -> Result<(), ApplyBlockError> {
        let mut wallet = self.inner.lock().unwrap();
        eprintln!("lib.rs: Got wallet");

        let tip = wallet.latest_checkpoint();
        eprintln!("lib.rs: Got tip {:?}", tip.block_id());
        eprintln!(
            "lib.rs: Found genesis from tip: {:?}",
            tip.clone().into_iter().last().unwrap().block_id()
        );
        if tip.height() == 0 {
            wallet
                .apply_block_connected_to(&block.0, height, tip.block_id())
                .map_err(|err| match err {
                    bdk::chain::local_chain::ApplyHeaderError::InconsistentBlocks => {
                        eprintln!("lib.rs: Inconsistent block!");
                        unreachable!("cannot happen")
                    }
                    bdk::chain::local_chain::ApplyHeaderError::CannotConnect(err) => {
                        ApplyBlockError::CannotConnect(err)
                    }
                })?;
        } else {
            eprintln!("lib.rs: Cloning out of block...");
            let block = block.0.clone();
            eprintln!("lib.rs: Actually applying...");
            wallet.apply_block(&block, height).map_err(|err| {
                eprintln!("lib.rs: Failed to apply block: {}", err);
                ApplyBlockError::CannotConnect(err)
            })?;
        }

        eprintln!("lib.rs: Commiting to db!");
        wallet.commit().map_err(ApplyBlockError::Database)?;
        Ok(())
    }
}

pub struct Block(bitcoin::Block);

impl Block {
    pub fn new(b: &[u8]) -> Self {
        let mut reader = b;
        Block(
            bitcoin::Block::consensus_decode_from_finite_reader(&mut reader)
                .expect("must decode block"),
        )
    }

    pub fn hash(&self) -> Vec<u8> {
        self.0.block_hash().as_byte_array().to_vec()
    }
}

pub struct BlockId {
    pub height: u32,
    pub hash: Vec<u8>,
}
