--
-- PostgreSQL database dump
--


-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: cargo_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo_stats (
    day_utc date NOT NULL,
    role character varying(32) NOT NULL,
    user_id uuid DEFAULT '00000000-0000-0000-0000-000000000000'::uuid NOT NULL,
    created_count bigint DEFAULT 0 NOT NULL,
    completed_count bigint DEFAULT 0 NOT NULL,
    cancelled_count bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: cargo_stats cargo_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_stats
    ADD CONSTRAINT cargo_stats_pkey PRIMARY KEY (day_utc, role, user_id);


--
-- PostgreSQL database dump complete
--


