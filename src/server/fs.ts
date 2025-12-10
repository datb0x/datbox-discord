import * as fs from "fs";
import * as path from "path";
import { Writable } from "stream";
import { createGunzip, createGzip } from "zlib";
import type DatboxNetwork from "./network";
import { createHash } from "crypto";

const FILE_CHUNK_SIZE = 10 * 1024 * 1023; // less than 10 MiB

export default class DatboxFileSystem {
	readonly root: string;
	readonly network: DatboxNetwork;

	constructor(root: string, network: DatboxNetwork) {
		fs.mkdirSync(root, { recursive: true });
		if (!fs.existsSync(root)) throw new Error("Failed to create root directory");
		this.root = root;
		this.network = network;
	}

	existsSync(file: string) {
		return fs.existsSync(path.join(this.root, file));
	}

	async uploadAsync(physPath: string, virtPath: string, progressCallback: (current: number, total: number) => void) {
		if (!fs.existsSync(physPath)) throw new Error("Failed to create directory");

		const stat = fs.statSync(physPath);
		if (!stat.isFile()) throw new Error("Only file uploads are currently supported");

		const virtDir = path.join(this.root, path.dirname(virtPath));
		fs.mkdirSync(path.join(this.root, path.dirname(virtPath)), { recursive: true });
		if (!fs.existsSync(virtDir)) throw new Error("Failed to create directory");

		if (fs.existsSync(path.join(this.root, virtPath))) throw new Error("File already exists in virtual file system");

		const buf = Buffer.alloc(FILE_CHUNK_SIZE);
		let bufLength = 0, chunks = 0;

		const writeStream = fs.createWriteStream(path.join(this.root, virtPath));
		// Write file size
		buf.writeUint32BE(stat.size);
		writeStream.write(Buffer.copyBytesFrom(buf, 0, 4));
		// Piggyback file checksum
		const hash = createHash("md5");

		const readStream = fs.createReadStream(physPath);
		const gzip = createGzip();
		const uploader = new Writable({
			write: (chunk, _encoding, callback) => {
				hash.update(chunk);
				const sending: Promise<string>[] = [];
				for (const byte of chunk as number[]) {
					buf.writeUInt8(byte, bufLength++);
					// Buffer is full. Send to Discord
					if (bufLength >= buf.byteLength) {
						const copy = Buffer.from(buf);
						sending.push(this.network.sendAttachment(copy));
						bufLength = 0;
					}
				}

				Promise.all(sending)
					.then((ids) => {
						const buffer = Buffer.alloc(8);
						for (const id of ids) {
							buffer.writeBigUInt64BE(BigInt(id), 0);
							writeStream.write(Buffer.from(buffer));
						}
						chunks += ids.length;
						callback();
					})
					.catch((err) => callback(err));
			},
		});

		const uploaded = new Promise<void>((res, rej) => uploader.on("close", () => {
			// Send last incomplete chunk
			if (bufLength > 0) {
				const copy = Buffer.copyBytesFrom(buf, 0, bufLength);
				this.network.sendAttachment(copy).then(id => {
					const buffer = Buffer.alloc(8);
					buffer.writeBigUInt64BE(BigInt(id), 0);
					chunks++;
					writeStream.write(buffer, (err) => {
						if (err) rej(err);
						else res();
					});
				}).catch(rej);
			} else res();
		}).on("error", rej));

		let totalBytes = 0;
		readStream.on("data", chunk => {
			totalBytes += chunk.length;
			progressCallback(totalBytes, stat.size);
		});
		readStream.pipe(gzip).pipe(uploader);

		await uploaded;

		// Write separator
		const sep = Buffer.alloc(8, 0);
		writeStream.write(sep);
		// Write file checksum at the end
		const checksum = hash.digest();
		writeStream.write(checksum);
		writeStream.close();

		return { path: path.join("/", virtPath), chunks, checksum };
	}

	async downloadAsync(virtPath: string, physPath: string, progressCallback: (current: number, total: number) => void) {
		if (fs.existsSync(physPath)) throw new Error(`Physical path ${physPath} already exists. Not overwriting`);
		if (!this.existsSync(virtPath)) throw new Error(`Virtual path ${path.join(this.root, virtPath)} doesn't exist`);

		const readStream = fs.createReadStream(path.join(this.root, virtPath));
		const writeStream = fs.createWriteStream(physPath);
		const gunzip = createGunzip();
		gunzip.pipe(writeStream);

		// Wait for readable
		await new Promise<void>(res => readStream.on("readable", () => res()));

		const size = (readStream.read(4) as Buffer).readUInt32BE();
		const hash = createHash("md5");

		let id: BigInt, totalBytes = 0;
		while (id = (readStream.read(8) as Buffer).readBigUInt64BE()) {
			const buf = await this.network.fetchAttachment(id.toString());
			gunzip.write(buf);
			hash.update(buf);
			totalBytes += buf.length;
			progressCallback(totalBytes, size);
		}

		gunzip.end();
		
		const newChecksum = hash.digest();
		const oldChecksum = readStream.read(newChecksum.byteLength) as Buffer;
		if (!oldChecksum || oldChecksum.byteLength != newChecksum.byteLength) throw new Error("Virtual file is corrupted");
		for (let ii = 0; ii < oldChecksum.byteLength; ii++)
			if (newChecksum[ii] != oldChecksum[ii])
				throw new Error("Downloaded file checksum doesn't match");
	}
}