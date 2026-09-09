export interface LiveSession {
  id: string;
  creator_id: string;
  creator_name?: string;
  handle?: string;
  title: string;
  description: string;
  category: string;
  status: "SCHEDULED" | "LIVE" | "ENDED" | "CANCELLED";
  visibility: "public" | "private";
  started_at?: string;
  ended_at?: string;
  peak_listeners: number;
  total_listeners: number;
  created_at: string;
}

export interface StreamToken {
  token: string;
  room_name: string;
  ws_url: string;
  expires_at: string;
}

export interface ChatMessage {
  id?: string;
  user_id: string;
  username: string;
  body: string;
  ts?: number;
}
