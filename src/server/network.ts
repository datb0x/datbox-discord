import { DiscordSnowflake } from "@sapphire/snowflake";
import { createHash } from "crypto";
import { AttachmentBuilder, Client, Events, TextChannel, type Channel, type Snowflake, type TextBasedChannel } from "discord.js";

export default class DatboxNetwork {
	client: Client;
	channelId: Snowflake;
	channel?: Channel | null;

	constructor(channelId: Snowflake) {
		this.channelId = channelId;
		this.client = new Client({
			intents: "MessageContent"
		});
	}

	async login(token: string) {
		const channel = new Promise<Channel | null>((res, rej) => {
			this.client.on(Events.ClientReady, (client) => {
				console.log(`${client.user.displayName} is ready`);
				this.client.channels.fetch(this.channelId).then(res).catch(rej);
			});
		});

		this.client.login(token);

		this.channel = await channel;
		if (!(this.channel instanceof TextChannel)) throw new Error("Supplied channel is not a text channel");
	}

	async sendAttachment(data: Buffer): Promise<string> {
		if (!this.client.isReady()) throw new Error("Client is not ready yet");

		const attachment = new AttachmentBuilder(data);
		// Fancy random name
		const hash = createHash("md5");
		hash.update(data);
		attachment.setName(hash.digest("hex"));
		const channel = this.channel as TextChannel;
		const message = await channel.send({ files: [attachment] });
		return message.id;
	}

	async fetchAttachment(id: string): Promise<Buffer> {
		if (!this.client.isReady()) throw new Error("Client is not ready yet");

		const channel = this.channel as TextChannel;
		const message = await channel.messages.fetch(id);
		const attachment = message.attachments.first();
		if (!attachment) throw new Error("Message has no attachment");
		const res = await fetch(attachment.url);
		if (!res.ok) throw new Error(`Received HTTP status ${res.status} while fetching attachment`);
		return Buffer.from(await res.arrayBuffer());
	}

	async deleteMessages(ids: (string | bigint)[]) {
		if (!this.client.isReady()) throw new Error("Client is not ready yet");

		const bulk: string[] = [];
		const individual: string[] = [];
		ids.forEach(id => {
			const data = DiscordSnowflake.deconstruct(id);
			if (BigInt(Date.now()) - data.timestamp < 14 * 24 * 60 * 60 * 1000) bulk.push(id.toString());
			else individual.push(id.toString());
		});

		const channel = this.channel as TextChannel;
		if (bulk.length) await channel.bulkDelete(bulk);
		for (const id of individual)
			await channel.messages.delete(id);
	}
}