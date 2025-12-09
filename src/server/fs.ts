import * as fs from "fs";
import * as path from "path";
import { Writable } from "stream";
import { createGzip } from "zlib";
import type DatboxNetwork from "./network";

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

	async uploadAsync(physPath: string, virtPath: string) {
		if (!fs.existsSync(physPath)) throw new Error("Failed to create directory");

		const stat = fs.statSync(physPath);
		if (!stat.isFile()) throw new Error("Only file uploads are currently supported");

		const virtDir = path.join(this.root, path.dirname(virtPath));
		fs.mkdirSync(path.join(this.root, path.dirname(virtPath)), { recursive: true });
		if (!fs.existsSync(virtDir)) throw new Error("Failed to create directory");

		if (fs.existsSync(path.join(this.root, virtPath))) throw new Error("File already exists in virtual file system");

		const buf = Buffer.alloc(FILE_CHUNK_SIZE);
		let bufLength = 0;

		const readStream = fs.createReadStream(physPath);
		const localWriteStream = fs.createWriteStream(path.join(this.root, virtPath));
		const gzip = createGzip();
		const uploader = new Writable({
			write: (chunk, _encoding, callback) => {
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
							localWriteStream.write(Buffer.from(buffer));
						}
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
					localWriteStream.write(buffer, (err) => {
						if (err) rej(err);
						else res();
					});
				}).catch(rej);
			} else res();
		}).on("error", rej));

		let totalBytes = 0;
		readStream.on("data", chunk => totalBytes += chunk.length);
		readStream.pipe(gzip).pipe(uploader);

		await uploaded;
	}
}