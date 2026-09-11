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

/** One episode in the subscriptions feed / creator page / history lists (Phase 4). */
export interface FeedItem {
  id: string;
  title: string;
  category: string;
  creator_id: string;
  creator_name: string;
  handle: string;
  duration_ms: number;
  published_at?: string | null;
}

export interface LikeState {
  liked: boolean;
  likes: number;
}

export interface CommentItem {
  id: string;
  audio_id: string;
  user_id: string;
  username: string;
  parent_id?: string | null;
  body: string;
  is_pinned: boolean;
  like_count: number;
  mine: boolean;
  created_at: string;
}

export interface CommentThread {
  comments: CommentItem[];
  replies: CommentItem[];
}

export interface HitAudio {
  id: string;
  title: string;
  category: string;
  creator_id: string;
  creator_name: string;
  handle: string;
  duration_ms: number;
  published_at?: string | null;
  rank: number;
}

export interface HitCreator {
  id: string;
  handle: string;
  channel_name: string;
  tagline: string;
  is_live: boolean;
  subscriber_count: number;
  rank: number;
}

export interface SearchResults {
  query: string;
  audio: HitAudio[];
  creators: HitCreator[];
}

export interface Creator {
  id: string;
  user_id: string;
  handle: string;
  channel_name: string;
  tagline: string;
  is_live: boolean;
  subscriber_count: number;
}

export interface CreatorPage {
  creator: Creator;
  episodes: FeedItem[];
  following: boolean;
}

export interface FollowState {
  following: boolean;
  subscribers: number;
}

export interface HistoryEntry {
  audio_id: string;
  title: string;
  category: string;
  creator_id: string;
  creator_name: string;
  handle: string;
  duration_ms: number;
  position_ms: number;
  completed: boolean;
  play_count: number;
  updated_at: string;
  published_at?: string | null;
}

export interface Playlist {
  id: string;
  user_id: string;
  owner_name?: string;
  title: string;
  description: string;
  visibility: "public" | "private";
  item_count: number;
  items?: PlaylistItem[];
  created_at: string;
  updated_at: string;
}

export interface PlaylistItem {
  audio_id: string;
  position: number;
  added_at: string;
  title: string;
  creator_name: string;
  handle: string;
  duration_ms: number;
  category: string;
  published_at?: string | null;
}

export interface StudioStats {
  episodes: number;
  total_likes: number;
  total_seconds: number;
  latest?: AudioItem;
  top_audio: {
    id: string;
    title: string;
    likes: number;
    published_at?: string | null;
    duration_ms: number;
  }[];
  recent_publishes: {
    id: string;
    title: string;
    published_at?: string | null;
  }[];
  live_sessions: {
    total: number;
    peak_listeners: number;
    total_listeners: number;
  };
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
