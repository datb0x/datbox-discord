export type ServerResponse<T> = {
	text?: string;
	error?: boolean;
	final?: boolean;
	data?: T;
}

export type NoDataResponse = ServerResponse<undefined>;

export type TransferResponse = ServerResponse<{
	current: number;
	total: number;
}>