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

export interface AudioSource {
  quality: string;
  url: string;
  bitrate_kbps: number;
}

export interface AudioItem {
  id: string;
  creator_id: string;
  creator_name?: string;
  handle?: string;
  title: string;
  description: string;
  category: string;
  status: string;
  duration_ms: number;
  sources?: AudioSource[];
  created_at: string;
  /** "upload" | "live_recording" — live recordings start as drafts. */
  source?: string;
  source_session_id?: string;
  visibility?: "public" | "private";
}

/** Status of the egress recording for an ended live session (Phase 3). */
export interface LiveRecording {
  id: string;
  session_id: string;
  egress_id: string;
  room_name: string;
  status: "RECORDING" | "ENDING" | "COMPLETED" | "FAILED" | "CONVERTED";
  storage_key?: string;
  file_size: number;
  /** Draft episode id once converted — publish it from the studio. */
  audio_id?: string;
  error?: string;
  started_at: string;
  completed_at?: string;
  created_at: string;
}
