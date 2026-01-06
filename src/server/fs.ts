import * as fs from "fs";
import * as path from "path";
import { Writable } from "stream";
import { createGunzip, createGzip } from "zlib";
import type DatboxNetwork from "./network";
import { createHash } from "crypto";
import { Sema } from "async-sema";

const FILE_CHUNK_SIZE = 10 * 1024 * 1023; // less than 10 MiB

export default class DatboxFileSystem {
	readonly dataDir: string;
	readonly root: string;
	readonly sema: Sema;
	readonly network: DatboxNetwork;
	readonly fileReference: Map<string, number>;

	constructor(dataDir: string, maxJobs: number, network: DatboxNetwork) {
		this.dataDir = dataDir;
		this.root = path.join(dataDir, "root");
		fs.mkdirSync(this.root, { recursive: true });
		if (!fs.existsSync(this.root)) throw new Error("Failed to create root directory");
		this.sema = new Sema(maxJobs);
		this.network = network;

		// Files where their hashes are the same, indicating a copy reference
		this.fileReference = new Map();
		try {
			const data = JSON.parse(fs.readFileSync(path.join(dataDir, "ref.json"), "utf8"));
			for (const key in data) {
				if (typeof data[key] != "number") continue;
				this.fileReference.set(key, data[key]);
			}
		} catch (_err) {
			// Ignored
		}
	}

	private sanitize(virtualPath: string) {
		const relative = path.relative(this.root, path.join(this.root, virtualPath));
		if (relative.startsWith("..")) return "/";
		return relative;
	}

	private async md5Async(physPath: string) {
		const stat = fs.statSync(physPath);
		if (!stat.isFile()) throw new Error("MD5 checksum can only be done on files");
		return await new Promise<string>((res, rej) => {
			const hash = createHash("md5");
			const readStream = fs.createReadStream(physPath);

			readStream.on("error", rej);
			readStream.on("data", (chunk) => hash.update(chunk));
			readStream.on("close", () => res(hash.digest("hex")));
		});
	}

	private saveReference() {
		try {
			const writeStream = fs.createWriteStream(path.join(this.dataDir, "ref.json"), "utf8");
			writeStream.write("{\n");
			let first = true;
			for (const [key, val] of this.fileReference) {
				if (first) first = false;
				else writeStream.write(",\n");
				writeStream.write(`\t"${key}": ${val}`);
			}
			writeStream.write("\n}");
		} catch (_err) {
			// Ignored
		}
	}

	private existsSync(file: string) {
		return fs.existsSync(path.join(this.root, file));
	}

	mkdirSync(dir: string, options?: { recursive?: boolean }) {
		dir = this.sanitize(dir);
		return fs.mkdirSync(path.join(this.root, dir), options);
	}

	async statAsync(file: string) {
		file = this.sanitize(file);
		const stat = fs.statSync(path.join(this.root, file));
		if (stat.isFile()) {
			const readStream = fs.createReadStream(path.join(this.root, file), { start: 0, end: 8 });
			// Wait for readable
			await new Promise<void>(res => readStream.on("readable", () => res()));
			const size = (readStream.read(8) as Buffer).readBigUInt64BE();
			stat.size = Number(size);
		}
		return stat;
	}

	async readdirAsync(file: string, long = false) {
		file = this.sanitize(file);
		if (fs.statSync(path.join(this.root, file)).isFile()) throw new Error(`Not a directory`);
		if (!long) return fs.readdirSync(path.join(this.root, file)).map(entry => ({
			name: entry,
			stat: fs.statSync(path.join(this.root, file, entry))
		}));
		else return await Promise.all(fs.readdirSync(path.join(this.root, file)).map(entry => new Promise<{ name: string, stat: fs.Stats }>((res, rej) => {
			this.statAsync(path.join(file, entry))
				.then((stat) => res({ name: entry, stat }))
				.catch(rej);
		})));
	}

	moveSync(src: string, dest: string) {
		src = this.sanitize(src);
		dest = this.sanitize(dest);
		if (!this.existsSync(src)) throw new Error("Source file doesn't exist");
		if (this.existsSync(dest)) throw new Error("Destination file already exists");

		fs.renameSync(path.join(this.root, src), path.join(this.root, dest));
	}

	async cpAsync(src: string, dest: string, options?: { recursive?: boolean }) {
		src = this.sanitize(src);
		dest = this.sanitize(dest);
		fs.cpSync(path.join(this.root, src), path.join(this.root, dest), { errorOnExist: true, recursive: options?.recursive });
		// For all files under src, add 1 to reference
		const recurse = async (dirOrFile: string) => {
			const stat = fs.statSync(dirOrFile);
			if (stat.isFile()) {
				const hash = await this.md5Async(dirOrFile);
				this.fileReference.set(hash, (this.fileReference.get(hash) || 1) + 1);
			} else if (stat.isDirectory())
				for (const file of fs.readdirSync(dirOrFile))
					await recurse(path.join(dirOrFile, file));
		};
		await recurse(path.join(this.root, src));
		this.saveReference();
	}

	async rmAsync(file: string, options?: { recursive?: boolean, remote?: boolean }) {
		file = this.sanitize(file);
		if (!this.existsSync(file)) throw new Error("File doesn't exist");
		const stat = fs.statSync(path.join(this.root, file));
		if (stat.isFile()) {
			const hash = await this.md5Async(path.join(this.root, file));
			const refs = (this.fileReference.get(hash) || 1) - 1;
			if (options?.remote) {
				if (refs) console.warn("Cannot delete remote. Another file referencing the same chunks exist");
				else {
					const readStream = fs.createReadStream(path.join(this.root, file), { start: 8 });
					// Wait for readable
					await new Promise<void>(res => readStream.on("readable", () => res()));

					let ids: bigint[] = [];
					let id: bigint;
					while (id = (readStream.read(8) as Buffer).readBigUInt64BE())
						ids.push(id);
					
					await this.network.deleteMessages(ids);
				}
			}
			fs.rmSync(path.join(this.root, file));
			if (refs > 1) this.fileReference.set(hash, refs);
			else this.fileReference.delete(hash);
		} else if (stat.isDirectory()) {
			if (!options?.recursive) throw new Error("Cannot remove directory. Consider setting recursive to true");
			for (const entry of fs.readdirSync(path.join(this.root, file)))
				await this.rmAsync(path.join(file, entry), options);
		}
		this.saveReference();
	}

	async uploadAsync(physPath: string, virtPath: string, progressCallback: (current: number, total: number) => void) {
		virtPath = this.sanitize(virtPath);
		if (!fs.existsSync(physPath)) throw new Error("Failed to create directory");

		const stat = fs.statSync(physPath);
		if (!stat.isFile()) throw new Error("Only file uploads are currently supported");

		const virtDir = path.join(this.root, path.dirname(virtPath));
		fs.mkdirSync(path.join(this.root, path.dirname(virtPath)), { recursive: true });
		if (!fs.existsSync(virtDir)) throw new Error("Failed to create directory");

		if (fs.existsSync(path.join(this.root, virtPath))) {
			if (fs.statSync(path.join(this.root, virtPath)).isDirectory()) virtPath = path.join(virtPath, path.basename(physPath));
			else throw new Error("File already exists in virtual file system");
		}

		await this.sema.acquire();
		try {
			const estimatedChunks = Math.ceil(stat.size / FILE_CHUNK_SIZE);
			console.log("Starting upload of", physPath);
			console.log("Estimated chunks (pre-gzip):", estimatedChunks);
			const buf = Buffer.alloc(FILE_CHUNK_SIZE);
			let bufLength = 0, chunks = 0;

			const writeStream = fs.createWriteStream(path.join(this.root, virtPath));
			// Write file size
			buf.writeBigUInt64BE(BigInt(stat.size));
			writeStream.write(Buffer.copyBytesFrom(buf, 0, 8));
			// Piggyback file checksum
			const hash = createHash("md5");

			const bufferedUpload: Promise<string>[] = [];
			const idBuf = Buffer.alloc(8);

			const readStream = fs.createReadStream(physPath);
			const gzip = createGzip();
			const uploader = new Writable({
				write: async (chunk, _encoding, callback) => {
					hash.update(chunk);
					for (const byte of chunk as number[]) {
						buf.writeUInt8(byte, bufLength++);
						// Buffer is full. Send to Discord
						if (bufLength >= buf.byteLength) {
							const copy = Buffer.from(buf);
							// Semaphore is taken. Wait for first upload to finish
							if (this.network.sema.tryAcquire() === undefined) {
								try {
									const id = await bufferedUpload.shift()!;
									idBuf.writeBigUInt64BE(BigInt(id), 0);
									writeStream.write(Buffer.from(idBuf));
									process.stdout.write(`\rUploaded chunks: ${++chunks} / ${estimatedChunks}`);
								} catch (err) {
									callback(err as Error);
									return;
								}
							}
							bufferedUpload.push(this.network.sendAttachment(copy));
							bufLength = 0;
						}
					}
					callback();
				},
			});

			const uploaded = new Promise<void>((res, rej) => uploader.on("close", () => {
				// Send last incomplete chunk
				if (bufLength > 0) {
					const copy = Buffer.copyBytesFrom(buf, 0, bufLength);
					bufferedUpload.push(this.network.sendAttachment(copy));
				}
				res();
			}).on("error", rej));

			let totalBytes = 0;
			readStream.on("data", chunk => {
				totalBytes += chunk.length;
				progressCallback(totalBytes, stat.size);
			});
			readStream.pipe(gzip).pipe(uploader);

			await uploaded;

			// Wait for all buffered uploads to resolve
			for (const upload of bufferedUpload) {
				idBuf.writeBigUInt64BE(BigInt(await upload));
				writeStream.write(Buffer.from(idBuf));
				process.stdout.write(`\rUploaded chunks: ${++chunks} / ${estimatedChunks}`);
			}
			process.stdout.write("\n");

			// Write separator
			const sep = Buffer.alloc(8, 0);
			writeStream.write(sep);
			// Write file checksum at the end
			const checksum = hash.digest();
			writeStream.write(checksum);
			writeStream.close();

			console.log("Finished upload of", physPath);
			this.sema.release();
			return { path: path.join("/", virtPath), chunks, checksum };
		} catch (err) {
			console.error(err);
			this.sema.release();
			throw err;
		}
	}

	async downloadAsync(virtPath: string, physPath: string, progressCallback: (current: number, total: number) => void) {
		virtPath = this.sanitize(virtPath);
		if (fs.existsSync(physPath)) throw new Error(`Physical path ${physPath} already exists. Not overwriting`);
		if (!this.existsSync(virtPath)) throw new Error(`Virtual path ${path.join(this.root, virtPath)} doesn't exist`);

		await this.sema.acquire();
		try {
			console.log("Starting download of", virtPath);
			const readStream = fs.createReadStream(path.join(this.root, virtPath));
			const writeStream = fs.createWriteStream(physPath);
			const gunzip = createGunzip();
			gunzip.pipe(writeStream);

			// Wait for readable
			await new Promise<void>(res => readStream.on("readable", () => res()));

			const size = Number((readStream.read(8) as Buffer).readBigUInt64BE());
			const hash = createHash("md5");

			const estimatedChunks = (fs.statSync(path.join(this.root, virtPath)).size - 24) / 8;
			console.log("File has size %d bytes. Estimated chunks (post-gzip): %d", size, estimatedChunks);

			// Setup gunzip to track progress
			let totalBytes = 0;
			gunzip.on("data", (chunk) => {
				totalBytes += chunk.length;
				progressCallback(totalBytes, size);
			});

			const bufferedDownload: Promise<Buffer>[] = [];

			let id: BigInt, chunks = 0;
			while (id = (readStream.read(8) as Buffer).readBigUInt64BE()) {
				// Sema used up. Wait for one download
				if (this.network.sema.tryAcquire() === undefined) {
					const buf = await bufferedDownload.shift()!;
					gunzip.write(buf);
					hash.update(buf);
					process.stdout.write(`\rDownloaded chunks: ${++chunks} / ${estimatedChunks}`);
				}
				bufferedDownload.push(this.network.fetchAttachment(id.toString()));
			}

			// Wait for all buffered downloads to resolve
			for (const download of bufferedDownload) {
				const buf = await download;
				gunzip.write(buf);
				hash.update(buf);
				process.stdout.write(`\rDownloaded chunks: ${++chunks} / ${estimatedChunks}`);
			}
			process.stdout.write("\n");

			gunzip.end();
			
			const newChecksum = hash.digest();
			const oldChecksum = readStream.read(newChecksum.byteLength) as Buffer;
			if (!oldChecksum || oldChecksum.byteLength != newChecksum.byteLength) throw new Error("Virtual file is corrupted");
			for (let ii = 0; ii < oldChecksum.byteLength; ii++)
				if (newChecksum[ii] != oldChecksum[ii])
					throw new Error("Downloaded file checksum doesn't match");

			console.log("Finished download of", virtPath);
			this.sema.release();
		} catch (err) {
			console.error(err);
			this.sema.release();
			throw err;
		}
	}
}