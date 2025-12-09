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
		const channel = this.channel as TextChannel;
		const message = await channel.send({ files: [attachment] });
		return message.id;
	}
}