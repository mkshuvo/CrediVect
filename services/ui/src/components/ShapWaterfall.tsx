"use client";

import React from 'react';
import { Box, Typography, Card, CardContent } from '@mui/material';

interface FeatureAttribution {
  feature_name: string;
  feature_value: string;
  shap_value: number;
  rank: number;
}

export default function ShapWaterfall({ attributions }: { attributions: FeatureAttribution[] }) {
  if (!attributions || attributions.length === 0) {
    return <Typography variant="body2" color="text.secondary">No SHAP attributions recorded.</Typography>;
  }

  const featureLabels: Record<string, string> = {
    revolving_utilization_bps: 'Revolving Credit Utilization',
    dti_bps: 'Debt-to-Income (DTI)',
    delinquencies_24m: '24-Month Delinquencies',
    inquiries_6m: '6-Month Hard Inquiries',
    age_of_oldest_trade_months: 'Age of Oldest Trade',
  };

  return (
    <Card variant="outlined">
      <CardContent>
        <Typography variant="h6" sx={{ mb: 1, fontWeight: 700 }}>
          TreeSHAP Feature Attribution Waterfall
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
          Local game-theoretic Shapley contributions. Positive values increase default risk (hurt score); negative values reduce default risk (boost score).
        </Typography>

        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          {attributions.map((attr) => {
            const isNegativeRisk = attr.shap_value < 0; // Reduces default risk = GOOD for applicant (Green)
            const label = featureLabels[attr.feature_name] || attr.feature_name;
            const barWidth = Math.min(Math.abs(attr.shap_value) * 120 + 20, 100);

            return (
              <Box key={attr.feature_name}>
                <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 0.5 }}>
                  <Typography variant="body2" sx={{ fontWeight: 600 }}>
                    #{attr.rank} {label} ({attr.feature_value})
                  </Typography>
                  <Typography
                    variant="body2"
                    sx={{
                      fontWeight: 700,
                      // The bar below can carry the bright hue; this is 14px
                      // text on white, and success.main (#10B981) is 2.5:1 and
                      // error.main (#EF4444) 3.8:1 there -- both far under the
                      // 4.5:1 floor. These are the same hues taken two steps
                      // darker: 5.5:1 and 6.4:1.
                      color: isNegativeRisk ? '#047857' : '#B91C1C',
                    }}
                  >
                    {isNegativeRisk ? 'Score Boost' : 'Risk Penalty'}: {attr.shap_value > 0 ? '+' : ''}{attr.shap_value.toFixed(3)}
                  </Typography>
                </Box>
                {/* Visual Waterfall Bar */}
                <Box sx={{ width: '100%', height: 12, bgcolor: '#E2E8F0', borderRadius: 6, overflow: 'hidden' }}>
                  <Box
                    sx={{
                      width: `${barWidth}%`,
                      height: '100%',
                      bgcolor: isNegativeRisk ? 'success.main' : 'error.main',
                      borderRadius: 6,
                    }}
                  />
                </Box>
              </Box>
            );
          })}
        </Box>
      </CardContent>
    </Card>
  );
}
