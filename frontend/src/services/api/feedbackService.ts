import { apiClient } from './client';

export interface ApiResponse<T = any> {
  success: boolean;
  data?: T;
  message?: string;
  error?: string;
}

export interface SubmitFeedbackRequest {
  alertId: string;
  responderId?: number;
  rating: number;
  feedback?: string;
  summaryNote?: string;
  wasHelpful?: boolean;
  feedbackType?: string;
}

export interface UserRatingStats {
  user_id: number;
  average_rating: number;
  total_ratings: number;
  five_star_count: number;
  four_star_count: number;
  three_star_count: number;
  two_star_count: number;
  one_star_count: number;
  // Only returned when viewing your own ratings.
  recent_feedback?: Array<{
    rating: number;
    feedback?: string;
    was_helpful?: boolean;
    created_at: string;
  }>;
}

export interface TopRatedUser {
  id: number;
  name: string;
  profile_picture_url?: string;
  average_rating: number;
  total_ratings: number;
}

export const feedbackService = {
  /**
   * Submit feedback for an alert
   */
  submitFeedback: async (data: SubmitFeedbackRequest): Promise<{ feedback_id: number }> => {
    const response = await apiClient.post<ApiResponse<{ feedback_id: number }>>('/feedback', {
      alert_id: data.alertId,
      responder_id: data.responderId,
      rating: data.rating,
      feedback: data.feedback,
      summary_note: data.summaryNote,
      was_helpful: data.wasHelpful,
      feedback_type: data.feedbackType || 'resolution',
    });

    const legacyFeedbackId = (response.data as ApiResponse<{ feedback_id: number }> & { feedback_id?: number }).feedback_id;

    if (!response.data.data && legacyFeedbackId) {
      return { feedback_id: legacyFeedbackId };
    }

    return response.data.data!;
  },

  /**
   * Get rating statistics for a user
   */
  getUserRatings: async (userId: number): Promise<UserRatingStats> => {
    const response = await apiClient.get<UserRatingStats>(`/users/${userId}/ratings`);
    return response.data;
  },

  /**
   * Get top rated users
   */
  getTopRatedUsers: async (limit: number = 10): Promise<TopRatedUser[]> => {
    const response = await apiClient.get<ApiResponse<{ users: TopRatedUser[]; count: number }>>(
      '/users/top-rated',
      { params: { limit } }
    );
    return response.data.data?.users || [];
  },
};
