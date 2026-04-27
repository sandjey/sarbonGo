--
-- PostgreSQL database dump
--

\restrict QkCWHlqd2eVabfMZYW8dTVLZKd2sAGRv0vQwQTt23v0eB0nkEwaSqMWlMEMKg00

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
-- Name: deleted_freelance_dispatchers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.deleted_freelance_dispatchers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    name character varying,
    phone character varying,
    password character varying,
    passport_series character varying,
    passport_number character varying,
    pinfl character varying,
    cargo_id uuid,
    driver_id uuid,
    rating double precision,
    work_status character varying,
    account_status character varying,
    photo_path character varying,
    created_at timestamp without time zone,
    updated_at timestamp without time zone,
    deleted_at timestamp without time zone,
    last_online_at timestamp without time zone,
    photo_data bytea,
    photo_content_type character varying(50),
    manager_role character varying(32)
);


--
-- Name: deleted_freelance_dispatchers deleted_freelance_dispatchers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deleted_freelance_dispatchers
    ADD CONSTRAINT deleted_freelance_dispatchers_pkey PRIMARY KEY (id);


--
-- Name: idx_deleted_freelance_dispatchers_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deleted_freelance_dispatchers_cargo_id ON public.deleted_freelance_dispatchers USING btree (cargo_id);


--
-- Name: idx_deleted_freelance_dispatchers_driver_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deleted_freelance_dispatchers_driver_id ON public.deleted_freelance_dispatchers USING btree (driver_id);


--
-- Name: idx_deleted_freelance_dispatchers_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deleted_freelance_dispatchers_phone ON public.deleted_freelance_dispatchers USING btree (phone);


--
-- Name: idx_deleted_freelance_dispatchers_pinfl; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deleted_freelance_dispatchers_pinfl ON public.deleted_freelance_dispatchers USING btree (pinfl);


--
-- PostgreSQL database dump complete
--

\unrestrict QkCWHlqd2eVabfMZYW8dTVLZKd2sAGRv0vQwQTt23v0eB0nkEwaSqMWlMEMKg00

